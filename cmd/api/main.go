package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var db *pgxpool.Pool

// Metrics instrumentation
var (
	httpRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ledger_http_requests_total",
		Help: "Total HTTP requests processed, labeled by status code",
	}, []string{"method", "endpoint", "status"})

	httpRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ledger_http_request_duration_seconds",
		Help:    "Latency distribution of HTTP requests",
		Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1},
	}, []string{"method", "endpoint"})
)

func main() {
	connString := os.Getenv("DB_SOURCE")
	if connString == "" {
		log.Fatal("DB_SOURCE environment variable is required")
	}

	var err error
	// Initialize connection pool
	db, err = pgxpool.New(context.Background(), connString)
	if err != nil {
		log.Fatalf("Database connection failure: %v\n", err)
	}
	defer db.Close()

	r := mux.NewRouter()

	// System endpoints
	r.HandleFunc("/healthz", HealthCheckHandler).Methods("GET")
	r.Handle("/metrics", promhttp.Handler()).Methods("GET")

	// Domain endpoints
	r.HandleFunc("/accounts", CreateAccountHandler).Methods("POST")
	r.HandleFunc("/transfers", CreateTransferHandler).Methods("POST")
	r.HandleFunc("/transfers/{id}", GetTransferHandler).Methods("GET")
	r.HandleFunc("/accounts/{id}", GetAccountHandler).Methods("GET")
	r.HandleFunc("/accounts/{id}/entries", GetAccountEntriesHandler).Methods("GET")

	log.Println("Service listening on :8080")
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

// CreateAccountHandler initializes a new account with zero balance.
func CreateAccountHandler(w http.ResponseWriter, r *http.Request) {
	var accountID int64
	query := "INSERT INTO accounts (balance) VALUES (0) RETURNING id"

	err := db.QueryRow(context.Background(), query).Scan(&accountID)
	if err != nil {
		log.Printf("Account creation failed: %v\n", err)
		http.Error(w, "System error creating account", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]int64{"account_id": accountID})
}

// Data Transfer Objects
type Account struct {
	ID      int64 `json:"id"`
	Balance int64 `json:"balance"`
}

type TransferRequest struct {
	FromAccountID int64 `json:"from_account_id"`
	ToAccountID   int64 `json:"to_account_id"`
	Amount        int64 `json:"amount"`
}

type Transfer struct {
	ID            int64  `json:"id"`
	FromAccountID int64  `json:"from_account_id"`
	ToAccountID   int64  `json:"to_account_id"`
	Amount        int64  `json:"amount"`
	Status        string `json:"status"`
}

type LedgerEntry struct {
	AccountID int64 `json:"account_id"`
	Delta     int64 `json:"delta"`
}

type TransferResponse struct {
	Transfer Transfer      `json:"transfer"`
	Entries  []LedgerEntry `json:"entries"`
}

// CreateTransferHandler executes an atomic double-entry transfer with idempotency.
func CreateTransferHandler(w http.ResponseWriter, r *http.Request) {
	timer := prometheus.NewTimer(httpRequestDuration.WithLabelValues("POST", "/transfers"))
	defer timer.ObserveDuration()

	ctx := context.Background()

	// Idempotency: Key Validation
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		httpRequestsTotal.WithLabelValues("POST", "/transfers", "400").Inc()
		respondWithError(w, http.StatusBadRequest, "Missing Idempotency-Key header")
		return
	}

	// Payload integrity check
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		httpRequestsTotal.WithLabelValues("POST", "/transfers", "500").Inc()
		respondWithError(w, http.StatusInternalServerError, "Stream read error")
		return
	}
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	hash := sha256.Sum256(bodyBytes)
	reqHash := hex.EncodeToString(hash[:])

	var req TransferRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		httpRequestsTotal.WithLabelValues("POST", "/transfers", "400").Inc()
		respondWithError(w, http.StatusBadRequest, "Malformed JSON body")
		return
	}

	// Business Rules
	if req.Amount <= 0 {
		httpRequestsTotal.WithLabelValues("POST", "/transfers", "422").Inc()
		respondWithError(w, http.StatusUnprocessableEntity, "Positive amount required")
		return
	}
	if req.FromAccountID == req.ToAccountID {
		httpRequestsTotal.WithLabelValues("POST", "/transfers", "422").Inc()
		respondWithError(w, http.StatusUnprocessableEntity, "Self-transfer not allowed")
		return
	}

	// Transaction: Start
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		log.Printf("Tx begin failed: %v\n", err)
		httpRequestsTotal.WithLabelValues("POST", "/transfers", "500").Inc()
		respondWithError(w, http.StatusInternalServerError, "Transaction initialization failure")
		return
	}
	defer tx.Rollback(ctx)

	// Idempotency: State Check
	var storedStatus int
	var storedBody json.RawMessage
	var storedHash string
	err = tx.QueryRow(ctx,
		"SELECT response_status, response_body, request_hash FROM idempotency_keys WHERE key = $1",
		idempotencyKey,
	).Scan(&storedStatus, &storedBody, &storedHash)

	if err == nil {
		// Key exists: Integrity Check
		if storedHash != reqHash {
			httpRequestsTotal.WithLabelValues("POST", "/transfers", "422").Inc()
			respondWithError(w, http.StatusUnprocessableEntity, "Key reuse with mismatched payload")
			return
		}

		// Valid Replay
		log.Printf("Replaying request: %s", idempotencyKey)
		w.Header().Set("Content-Type", "application/json")
		httpRequestsTotal.WithLabelValues("POST", "/transfers", "200").Inc()
		w.WriteHeader(http.StatusOK)
		w.Write(storedBody)
		return

	} else if err != pgx.ErrNoRows {
		log.Printf("Idempotency query error: %v\n", err)
		httpRequestsTotal.WithLabelValues("POST", "/transfers", "500").Inc()
		respondWithError(w, http.StatusInternalServerError, "Storage error")
		return
	}

	// Idempotency: Reservation
	_, err = tx.Exec(ctx,
		"INSERT INTO idempotency_keys (key, request_hash, status) VALUES ($1, $2, 'in_progress')",
		idempotencyKey, reqHash,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // Unique violation
			httpRequestsTotal.WithLabelValues("POST", "/transfers", "409").Inc()
			respondWithError(w, http.StatusConflict, "Request processing in progress")
		} else {
			log.Printf("Key reservation error: %v\n", err)
			httpRequestsTotal.WithLabelValues("POST", "/transfers", "500").Inc()
			respondWithError(w, http.StatusInternalServerError, "Storage error")
		}
		return
	}

	// Concurrency Control: Deterministic Locking
	// Sort IDs to prevent circular wait (deadlock)
	var acc1_id, acc2_id = req.FromAccountID, req.ToAccountID
	if acc1_id > acc2_id {
		acc1_id, acc2_id = req.ToAccountID, req.FromAccountID
	}

	var balance1, balance2 int64
	// Acquire locks in order
	err = tx.QueryRow(ctx, "SELECT balance FROM accounts WHERE id = $1 FOR UPDATE", acc1_id).Scan(&balance1)
	if err != nil {
		httpRequestsTotal.WithLabelValues("POST", "/transfers", "404").Inc()
		respondWithError(w, http.StatusNotFound, "Account not found")
		return
	}
	err = tx.QueryRow(ctx, "SELECT balance FROM accounts WHERE id = $1 FOR UPDATE", acc2_id).Scan(&balance2)
	if err != nil {
		httpRequestsTotal.WithLabelValues("POST", "/transfers", "404").Inc()
		respondWithError(w, http.StatusNotFound, "Account not found")
		return
	}

	// Logic: Funds Check
	var fromBalance int64
	if req.FromAccountID == acc1_id {
		fromBalance = balance1
	} else {
		fromBalance = balance2
	}

	if fromBalance < req.Amount {
		httpRequestsTotal.WithLabelValues("POST", "/transfers", "422").Inc()
		respondWithError(w, http.StatusUnprocessableEntity, "Insufficient funds")
		return
	}

	// Execution: Ledger Update
	var transferID int64
	err = tx.QueryRow(ctx,
		"INSERT INTO transfers (from_account_id, to_account_id, amount, status) VALUES ($1, $2, $3, 'completed') RETURNING id",
		req.FromAccountID, req.ToAccountID, req.Amount,
	).Scan(&transferID)
	if err != nil {
		log.Printf("Transfer insert error: %v\n", err)
		httpRequestsTotal.WithLabelValues("POST", "/transfers", "500").Inc()
		respondWithError(w, http.StatusInternalServerError, "Write error")
		return
	}

	// Immutable Double-Entry Logs
	_, err = tx.Exec(ctx,
		"INSERT INTO ledger_entries (transfer_id, account_id, delta) VALUES ($1, $2, $3), ($1, $4, $5)",
		transferID, req.FromAccountID, -req.Amount, req.ToAccountID, req.Amount,
	)
	if err != nil {
		log.Printf("Ledger entry error: %v\n", err)
		httpRequestsTotal.WithLabelValues("POST", "/transfers", "500").Inc()
		respondWithError(w, http.StatusInternalServerError, "Write error")
		return
	}

	// Balance Updates
	_, err = tx.Exec(ctx, "UPDATE accounts SET balance = balance - $1 WHERE id = $2", req.Amount, req.FromAccountID)
	if err != nil {
		return
	}
	_, err = tx.Exec(ctx, "UPDATE accounts SET balance = balance + $1 WHERE id = $2", req.Amount, req.ToAccountID)
	if err != nil {
		return
	}

	// Idempotency: Finalize
	resp := TransferResponse{
		Transfer: Transfer{
			ID:            transferID,
			FromAccountID: req.FromAccountID,
			ToAccountID:   req.ToAccountID,
			Amount:        req.Amount,
			Status:        "completed",
		},
		Entries: []LedgerEntry{
			{AccountID: req.FromAccountID, Delta: -req.Amount},
			{AccountID: req.ToAccountID, Delta: req.Amount},
		},
	}

	respBody, err := json.Marshal(resp)
	if err != nil {
		return
	}

	_, err = tx.Exec(ctx,
		"UPDATE idempotency_keys SET status = 'completed', transfer_id = $1, response_status = $2, response_body = $3 WHERE key = $4",
		transferID, http.StatusCreated, respBody, idempotencyKey,
	)
	if err != nil {
		log.Printf("Idempotency update error: %v\n", err)
		httpRequestsTotal.WithLabelValues("POST", "/transfers", "500").Inc()
		respondWithError(w, http.StatusInternalServerError, "Commit error")
		return
	}

	// Commit
	if err = tx.Commit(ctx); err != nil {
		log.Printf("Tx commit error: %v\n", err)
		httpRequestsTotal.WithLabelValues("POST", "/transfers", "500").Inc()
		respondWithError(w, http.StatusInternalServerError, "Commit error")
		return
	}

	w.Header().Set("Location", fmt.Sprintf("/transfers/%d", transferID))
	httpRequestsTotal.WithLabelValues("POST", "/transfers", "201").Inc()
	respondWithJSON(w, http.StatusCreated, resp)
}

func GetTransferHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var transfer Transfer
	err := db.QueryRow(context.Background(),
		"SELECT id, from_account_id, to_account_id, amount, status FROM transfers WHERE id = $1",
		id).Scan(&transfer.ID, &transfer.FromAccountID, &transfer.ToAccountID, &transfer.Amount, &transfer.Status)

	if err == pgx.ErrNoRows {
		httpRequestsTotal.WithLabelValues("GET", "/transfers/{id}", "404").Inc()
		respondWithError(w, http.StatusNotFound, "Transfer not found")
		return
	} else if err != nil {
		httpRequestsTotal.WithLabelValues("GET", "/transfers/{id}", "500").Inc()
		respondWithError(w, http.StatusInternalServerError, "Read error")
		return
	}

	httpRequestsTotal.WithLabelValues("GET", "/transfers/{id}", "200").Inc()
	respondWithJSON(w, http.StatusOK, transfer)
}

func GetAccountHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var account Account
	err := db.QueryRow(context.Background(),
		"SELECT id, balance FROM accounts WHERE id = $1",
		id).Scan(&account.ID, &account.Balance)

	if err == pgx.ErrNoRows {
		httpRequestsTotal.WithLabelValues("GET", "/accounts/{id}", "404").Inc()
		respondWithError(w, http.StatusNotFound, "Account not found")
		return
	} else if err != nil {
		httpRequestsTotal.WithLabelValues("GET", "/accounts/{id}", "500").Inc()
		respondWithError(w, http.StatusInternalServerError, "Read error")
		return
	}

	httpRequestsTotal.WithLabelValues("GET", "/accounts/{id}", "200").Inc()
	respondWithJSON(w, http.StatusOK, account)
}

func GetAccountEntriesHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	accountID := vars["id"]

	// Validate account existence to differentiate 404 from empty history
	var exists bool
	err := db.QueryRow(context.Background(), "SELECT EXISTS(SELECT 1 FROM accounts WHERE id=$1)", accountID).Scan(&exists)
	if err != nil || !exists {
		httpRequestsTotal.WithLabelValues("GET", "/accounts/{id}/entries", "404").Inc()
		respondWithError(w, http.StatusNotFound, "Account not found")
		return
	}

	rows, err := db.Query(context.Background(),
		"SELECT account_id, delta FROM ledger_entries WHERE account_id = $1 ORDER BY created_at DESC",
		accountID)
	if err != nil {
		httpRequestsTotal.WithLabelValues("GET", "/accounts/{id}/entries", "500").Inc()
		respondWithError(w, http.StatusInternalServerError, "Read error")
		return
	}
	defer rows.Close()

	var entries []LedgerEntry
	for rows.Next() {
		var entry LedgerEntry
		if err := rows.Scan(&entry.AccountID, &entry.Delta); err != nil {
			continue
		}
		entries = append(entries, entry)
	}

	httpRequestsTotal.WithLabelValues("GET", "/accounts/{id}/entries", "200").Inc()
	respondWithJSON(w, http.StatusOK, entries)
}
