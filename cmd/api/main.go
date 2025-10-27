package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Global variable for the database connection pool
var db *pgxpool.Pool

func main() {
	// 1. Connect to the database
	// Get the connection string from the 'DB_SOURCE' environment variable
	// set in docker-compose.yml
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

	// 2. Create a new router
	r := mux.NewRouter()

	// 3. Define your API endpoints
	r.HandleFunc("/healthz", HealthCheckHandler).Methods("GET")
	r.HandleFunc("/accounts", CreateAccountHandler).Methods("POST")
	r.HandleFunc("/transfers", CreateTransferHandler).Methods("POST")

	// 4. Start the server
	log.Println("Starting server on :8080")
	log.Fatal(http.ListenAndServe(":8080", r))
}

// HealthCheckHandler is a simple check to see if the server is running
func HealthCheckHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// CreateAccountHandler creates a new account with a zero balance
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

// CreateTransferHandler handles the core transfer logic
func CreateTransferHandler(w http.ResponseWriter, r *http.Request) {
	var req TransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	tx, err := db.Begin(context.Background())
	if err != nil {
		log.Printf("Failed to start transaction: %v\n", err)
		http.Error(w, "Failed to start transaction", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(context.Background())

	// === DEADLOCK PREVENTION ===
	// Lock accounts in a consistent order (smallest ID first)
	var acc1_id, acc2_id = req.FromAccountID, req.ToAccountID
	if acc1_id > acc2_id {
		acc1_id, acc2_id = req.ToAccountID, req.FromAccountID
	}

	// Lock the *first* account
	var balance1 int64
	err = tx.QueryRow(context.Background(), "SELECT balance FROM accounts WHERE id = $1 FOR UPDATE", acc1_id).Scan(&balance1)
	if err != nil {
		log.Printf("Account 1 (%d) not found: %v\n", acc1_id, err)
		http.Error(w, "Account 1 not found", http.StatusInternalServerError)
		return
	}

	// Lock the *second* account
	var balance2 int64
	err = tx.QueryRow(context.Background(), "SELECT balance FROM accounts WHERE id = $1 FOR UPDATE", acc2_id).Scan(&balance2)
	if err != nil {
		log.Printf("Account 2 (%d) not found: %v\n", acc2_id, err)
		http.Error(w, "Account 2 not found", http.StatusInternalServerError)
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
		http.Error(w, "Insufficient funds", http.StatusBadRequest)
		return
	}

	// Create the ledger entries
	_, err = tx.Exec(context.Background(),
		"INSERT INTO ledger_entries (debit_account_id, credit_account_id, amount) VALUES ($1, $2, $3)",
		req.FromAccountID, req.ToAccountID, req.Amount)
	if err != nil {
		log.Printf("Failed to create ledger entry: %v\n", err)
		http.Error(w, "Failed to create ledger entry", http.StatusInternalServerError)
		return
	}

	// Update balances
	_, err = tx.Exec(context.Background(),
		"UPDATE accounts SET balance = balance - $1 WHERE id = $2",
		req.Amount, req.FromAccountID)
	if err != nil {
		log.Printf("Failed to update sender balance: %v\n", err)
		http.Error(w, "Failed to update sender balance", http.StatusInternalServerError)
		return
	}

	_, err = tx.Exec(context.Background(),
		"UPDATE accounts SET balance = balance + $1 WHERE id = $2",
		req.Amount, req.ToAccountID)
	if err != nil {
		log.Printf("Failed to update receiver balance: %v\n", err)
		http.Error(w, "Failed to update receiver balance", http.StatusInternalServerError)
		return
	}

	// Commit the transaction
	if err = tx.Commit(context.Background()); err != nil {
		log.Printf("Failed to commit transaction: %v\n", err)
		http.Error(w, "Failed to commit transaction", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "transfer successful"})
}
