package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/punchamoorthee/ledgerops/internal/domain"
)

var (
	ErrAccountNotFound = errors.New("account not found")
	ErrConflict        = errors.New("conflict: request in progress")
	ErrKeyMismatch     = errors.New("idempotency key mismatch")
	ErrFunds           = errors.New("insufficient funds")
)

type LedgerStore struct {
	db *pgxpool.Pool
}

func NewLedgerStore(db *pgxpool.Pool) *LedgerStore {
	return &LedgerStore{db: db}
}

// ExecTransfer executes a double-entry transfer with strong consistency guarantees.
// 1. Enforces Idempotency (Exactly-Once)
// 2. Uses Deterministic Locking (Deadlock Prevention)
// 3. Enforces DB Invariants (Constraint Triggers)
func (s *LedgerStore) ExecTransfer(ctx context.Context, req domain.TransferRequest, idempotencyKey, reqHash string) (*domain.TransferResponse, error) {
	// Start Tx with Repeatable Read isolation to ensure consistent snapshots
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// --- 1. IDEMPOTENCY CHECK ---
	var storedStatus string
	var storedBody json.RawMessage
	var storedHash string

	err = tx.QueryRow(ctx,
		"SELECT status, response_body, request_hash FROM idempotency_keys WHERE key = $1",
		idempotencyKey).Scan(&storedStatus, &storedBody, &storedHash)

	if err == nil {
		// Key exists
		if storedHash != reqHash {
			return nil, ErrKeyMismatch
		}
		if storedStatus == "in_progress" {
			return nil, ErrConflict
		}
		// Return cached response
		var resp domain.TransferResponse
		if err := json.Unmarshal(storedBody, &resp); err != nil {
			return nil, err
		}
		return &resp, nil // Commit is not needed for read-only return
	} else if err != pgx.ErrNoRows {
		return nil, err
	}

	// Insert "in_progress" marker
	_, err = tx.Exec(ctx,
		"INSERT INTO idempotency_keys (key, request_hash, status) VALUES ($1, $2, 'in_progress')",
		idempotencyKey, reqHash)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // Unique violation
			return nil, ErrConflict
		}
		return nil, err
	}

	// --- 2. DETERMINISTIC LOCKING ---
	// Sort IDs to prevent circular wait conditions (Deadlock Freedom)
	first, second := req.FromAccountID, req.ToAccountID
	if first > second {
		first, second = second, first
	}

	// Acquire locks in ascending order
	// Use NOWAIT to fail fast during extreme contention scenarios (Hot-Spot)
	for _, id := range []int64{first, second} {
		var b int64
		if err := tx.QueryRow(ctx, "SELECT balance FROM accounts WHERE id = $1 FOR UPDATE NOWAIT", id).Scan(&b); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "55P03" { // Lock not available
				return nil, ErrConflict
			}
			return nil, ErrAccountNotFound
		}
	}

	// --- 3. BUSINESS LOGIC & EXECUTION ---
	var fromBalance int64
	if err := tx.QueryRow(ctx, "SELECT balance FROM accounts WHERE id = $1", req.FromAccountID).Scan(&fromBalance); err != nil {
		return nil, err
	}
	if fromBalance < req.Amount {
		return nil, ErrFunds
	}

	// Create Transfer Record
	var transferID int64
	err = tx.QueryRow(ctx,
		"INSERT INTO transfers (from_account_id, to_account_id, amount, status) VALUES ($1, $2, $3, 'completed') RETURNING id",
		req.FromAccountID, req.ToAccountID, req.Amount).Scan(&transferID)
	if err != nil {
		return nil, err
	}

	// Create Double-Entry Ledger Records (Debit and Credit)
	// The DB trigger `check_ledger_invariant` will verify SUM(delta) == 0 at COMMIT time.
	_, err = tx.Exec(ctx,
		"INSERT INTO ledger_entries (transfer_id, account_id, delta) VALUES ($1, $2, $3), ($1, $4, $5)",
		transferID, req.FromAccountID, -req.Amount, req.ToAccountID, req.Amount)
	if err != nil {
		return nil, fmt.Errorf("invariant violation: %v", err)
	}

	// Update Balances
	_, err = tx.Exec(ctx, "UPDATE accounts SET balance = balance - $1 WHERE id = $2", req.Amount, req.FromAccountID)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, "UPDATE accounts SET balance = balance + $1 WHERE id = $2", req.Amount, req.ToAccountID)
	if err != nil {
		return nil, err
	}

	// --- 4. FINALIZE ---
	resp := domain.TransferResponse{
		Transfer: domain.Transfer{ID: transferID, FromAccountID: req.FromAccountID, ToAccountID: req.ToAccountID, Amount: req.Amount, Status: "completed"},
		Entries: []domain.LedgerEntry{
			{AccountID: req.FromAccountID, Delta: -req.Amount},
			{AccountID: req.ToAccountID, Delta: req.Amount},
		},
	}

	respBytes, _ := json.Marshal(resp)
	_, err = tx.Exec(ctx,
		"UPDATE idempotency_keys SET status = 'completed', transfer_id = $1, response_status = 201, response_body = $2 WHERE key = $3",
		transferID, respBytes, idempotencyKey)
	if err != nil {
		return nil, err
	}

	return &resp, tx.Commit(ctx)
}

func (s *LedgerStore) CreateAccount(ctx context.Context, initialBalance int64) (int64, error) {
	var id int64
	err := s.db.QueryRow(ctx, "INSERT INTO accounts (balance) VALUES ($1) RETURNING id", initialBalance).Scan(&id)
	return id, err
}

func (s *LedgerStore) GetAccount(ctx context.Context, id int64) (*domain.Account, error) {
	var acc domain.Account
	err := s.db.QueryRow(ctx, "SELECT id, balance, created_at FROM accounts WHERE id = $1", id).Scan(&acc.ID, &acc.Balance, &acc.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, ErrAccountNotFound
	}
	return &acc, err
}
