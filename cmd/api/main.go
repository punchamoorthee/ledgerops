package main

import (
	"bytes" // For reading and hashing request body
	"context"
	"crypto/sha256" // For request_hash
	"encoding/hex"  // For request_hash
	"encoding/json"
	"errors"
	"fmt" // For setting Location header
	"io"  // For reading request body
	"log"
	"net/http"
	"os"

	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Global variable for the database connection pool
var db *pgxpool.Pool

func main() {
	connString := os.Getenv("DB_SOURCE")
	if connString == "" {
		log.Fatal("DB_SOURCE environment variable is not set")
	}

	var err error
	db, err = pgxpool.New(context.Background(), connString)
	if err != nil {
		log.Fatalf("Unable to connect to database: %v\n", err)
	}
	defer db.Close()

	r := mux.NewRouter()

	r.HandleFunc("/healthz", HealthCheckHandler).Methods("GET")
	r.HandleFunc("/accounts", CreateAccountHandler).Methods("POST")
	r.HandleFunc("/transfers", CreateTransferHandler).Methods("POST")

	r.Handle("/metrics", promhttp.Handler()).Methods("GET")

	log.Println("Starting server on :8080")
	log.Fatal(http.ListenAndServe(":8080", r))
}

func HealthCheckHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func respondWithError(w http.ResponseWriter, code int, message string) {
	respondWithJSON(w, code, map[string]string{"error": message})
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if payload != nil {
		json.NewEncoder(w).Encode(payload)
	}
}

func CreateAccountHandler(w http.ResponseWriter, r *http.Request) {
	var accountID int64
	query := "INSERT INTO accounts (balance) VALUES (0) RETURNING id"

	err := db.QueryRow(context.Background(), query).Scan(&accountID)
	if err != nil {
		log.Printf("Failed to create account: %v\n", err) // Log the error
		http.Error(w, "Failed to create account", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]int64{"account_id": accountID})
}

// Represents the JSON we expect from the user
type TransferRequest struct {
	FromAccountID int64 `json:"from_account_id"`
	ToAccountID   int64 `json:"to_account_id"`
	Amount        int64 `json:"amount"`
}

// Transfer represents the 'transfers' table record
type Transfer struct {
	ID            int64  `json:"id"`
	FromAccountID int64  `json:"from_account_id"`
	ToAccountID   int64  `json:"to_account_id"`
	Amount        int64  `json:"amount"`
	Status        string `json:"status"`
}

// LedgerEntry represents a single leg of the double-entry
type LedgerEntry struct {
	AccountID int64 `json:"account_id"`
	Delta     int64 `json:"delta"`
}

// TransferResponse is the canonical successful response
type TransferResponse struct {
	Transfer Transfer      `json:"transfer"`
	Entries  []LedgerEntry `json:"entries"`
}

func CreateTransferHandler(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()

	// === IDEMPOTENCY (GATE) ===
	// 1. Get the idempotency key from the header.
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		respondWithError(w, http.StatusBadRequest, "Idempotency-Key header is required")
		return
	}

	// 2. Read and HASH the request body for idempotency check
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Cannot read request body")
		return
	}
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	hash := sha256.Sum256(bodyBytes)
	reqHash := hex.EncodeToString(hash[:])

	// 3. Parse the request body
	var req TransferRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// 4. Validate business rules
	if req.Amount <= 0 {
		respondWithError(w, http.StatusUnprocessableEntity, "Amount must be positive")
		return
	}
	if req.FromAccountID == req.ToAccountID {
		respondWithError(w, http.StatusUnprocessableEntity, "Cannot transfer to the same account")
		return
	}

	// === ATOMIC TRANSACTION (BEGIN) ===
	// 5. Begin a new transaction with REPEATABLE READ isolation
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		log.Printf("Failed to start transaction: %v\n", err)
		respondWithError(w, http.StatusInternalServerError, "Failed to start transaction")
		return
	}
	defer tx.Rollback(ctx)

	// 6. === IDEMPOTENCY (CHECK) ===
	// Check if this key has already been processed.
	var storedStatus int
	var storedBody json.RawMessage
	var storedHash string
	err = tx.QueryRow(ctx,
		"SELECT response_status, response_body, request_hash FROM idempotency_keys WHERE key = $1",
		idempotencyKey,
	).Scan(&storedStatus, &storedBody, &storedHash)

	if err == nil {
		// CHECK HASH: Ensure this isn't a key reuse with a different body.
		if storedHash != reqHash {
			respondWithError(w, http.StatusUnprocessableEntity, "Idempotency-Key reused with different request")
			return
		}

		// This is a valid replay.
		log.Printf("Replaying idempotent request: %s", idempotencyKey)
		w.Header().Set("Content-Type", "application/json")

		// Note: We are replaying with 200 OK, not the original status code.
		w.WriteHeader(http.StatusOK)
		w.Write(storedBody)
		return

	} else if err != pgx.ErrNoRows {
		log.Printf("Failed to query idempotency key: %v\n", err)
		respondWithError(w, http.StatusInternalServerError, "Database error")
		return
	}

	// 7. === IDEMPOTENCY (RECORD) ===
	// Record the key as 'in_progress' to prevent a concurrent duplicate.
	_, err = tx.Exec(ctx,
		"INSERT INTO idempotency_keys (key, request_hash, status) VALUES ($1, $2, 'in_progress')",
		idempotencyKey, reqHash,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // 23505 = unique_violation
			// This handles the race condition.
			respondWithError(w, http.StatusConflict, "Duplicate request in progress")
		} else {
			log.Printf("Failed to insert idempotency key: %v\n", err)
			respondWithError(w, http.StatusInternalServerError, "Database error")
		}
		return
	}

	// 8. === CORE TRANSFER LOGIC ===

	// === DEADLOCK PREVENTION ===
	var acc1_id, acc2_id = req.FromAccountID, req.ToAccountID
	if acc1_id > acc2_id {
		acc1_id, acc2_id = req.ToAccountID, req.FromAccountID
	}

	var balance1 int64
	err = tx.QueryRow(ctx, "SELECT balance FROM accounts WHERE id = $1 FOR UPDATE", acc1_id).Scan(&balance1)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "Account 1 not found")
		return
	}

	var balance2 int64
	err = tx.QueryRow(ctx, "SELECT balance FROM accounts WHERE id = $1 FOR UPDATE", acc2_id).Scan(&balance2)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "Account 2 not found")
		return
	}

	// Check for sufficient funds
	var fromBalance int64
	if req.FromAccountID == acc1_id {
		fromBalance = balance1
	} else {
		fromBalance = balance2
	}

	if fromBalance < req.Amount {
		respondWithError(w, http.StatusUnprocessableEntity, "Insufficient funds")
		return
	}

	// --- Execute the Double-Entry SQL ---

	// 1. Create the 'transfers' record
	var transferID int64
	err = tx.QueryRow(ctx,
		"INSERT INTO transfers (from_account_id, to_account_id, amount, status) VALUES ($1, $2, $3, 'completed') RETURNING id",
		req.FromAccountID, req.ToAccountID, req.Amount,
	).Scan(&transferID)
	if err != nil {
		log.Printf("Failed to create transfer: %v\n", err)
		respondWithError(w, http.StatusInternalServerError, "Failed to create transfer")
		return
	}

	// 2. Create the debit leg (negative delta)
	_, err = tx.Exec(ctx,
		"INSERT INTO ledger_entries (transfer_id, account_id, delta) VALUES ($1, $2, $3)",
		transferID, req.FromAccountID, -req.Amount,
	)
	if err != nil {
		log.Printf("Failed to create debit entry: %v\n", err)
		respondWithError(w, http.StatusInternalServerError, "Failed to create ledger entry")
		return
	}

	// 3. Create the credit leg (positive delta)
	_, err = tx.Exec(ctx,
		"INSERT INTO ledger_entries (transfer_id, account_id, delta) VALUES ($1, $2, $3)",
		transferID, req.ToAccountID, req.Amount,
	)
	if err != nil {
		log.Printf("Failed to create credit entry: %v\n", err)
		respondWithError(w, http.StatusInternalServerError, "Failed to create ledger entry")
		return
	}

	// 4. Update sender balance
	_, err = tx.Exec(ctx,
		"UPDATE accounts SET balance = balance - $1 WHERE id = $2",
		req.Amount, req.FromAccountID,
	)
	if err != nil {
		log.Printf("Failed to update sender balance: %v\n", err)
		respondWithError(w, http.StatusInternalServerError, "Failed to update balance")
		return
	}

	// 5. Update receiver balance
	_, err = tx.Exec(ctx,
		"UPDATE accounts SET balance = balance + $1 WHERE id = $2",
		req.Amount, req.ToAccountID,
	)
	if err != nil {
		log.Printf("Failed to update receiver balance: %v\n", err)
		respondWithError(w, http.StatusInternalServerError, "Failed to update balance")
		return
	}

	// 9. === IDEMPOTENCY (FINALIZE) ===
	// Create the canonical response using the NEW nested structs
	resp := TransferResponse{
		Transfer: Transfer{
			ID:            transferID,
			FromAccountID: req.FromAccountID,
			ToAccountID:   req.ToAccountID,
			Amount:        req.Amount,
			Status:        "completed", // From the SQL INSERT
		},
		Entries: []LedgerEntry{
			{AccountID: req.FromAccountID, Delta: -req.Amount},
			{AccountID: req.ToAccountID, Delta: req.Amount},
		},
	}

	respBody, err := json.Marshal(resp)
	if err != nil {
		log.Printf("Failed to marshal response: %v\n", err)
		respondWithError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	// Atomically update the key to 'completed'
	_, err = tx.Exec(ctx,
		"UPDATE idempotency_keys SET status = 'completed', transfer_id = $1, response_status = $2, response_body = $3 WHERE key = $4",
		transferID, http.StatusCreated, respBody, idempotencyKey,
	)
	if err != nil {
		log.Printf("Failed to finalize idempotency key: %v\n", err)
		respondWithError(w, http.StatusInternalServerError, "Failed to finalize transaction")
		return
	}

	// 10. === COMMIT ===
	if err = tx.Commit(ctx); err != nil {
		log.Printf("Failed to commit transaction: %v\n", err)
		respondWithError(w, http.StatusInternalServerError, "Failed to commit transaction")
		return
	}

	// 11. Respond with 201 Created (for a NEW request)
	w.Header().Set("Location", fmt.Sprintf("/transfers/%d", transferID))
	respondWithJSON(w, http.StatusCreated, resp)
}
