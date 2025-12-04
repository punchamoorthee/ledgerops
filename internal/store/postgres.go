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
	ErrConflict        = errors.New("conflict")
)

type LedgerStore struct {
	db *pgxpool.Pool
}

func NewLedgerStore(db *pgxpool.Pool) *LedgerStore {
	return &LedgerStore{db: db}
}

func (s *LedgerStore) CreateAccount(ctx context.Context, initialBalance int64) (int64, error) {
	var id int64
	err := s.db.QueryRow(ctx, "INSERT INTO accounts (balance) VALUES ($1) RETURNING id", initialBalance).Scan(&id)
	return id, err
}

func (s *LedgerStore) GetAccount(ctx context.Context, id int64) (*domain.Account, error) {
	var acc domain.Account
	err := s.db.QueryRow(ctx, "SELECT id, balance FROM accounts WHERE id = $1", id).Scan(&acc.ID, &acc.Balance)
	if err == pgx.ErrNoRows {
		return nil, ErrAccountNotFound
	}
	return &acc, err
}

func (s *LedgerStore) GetEntries(ctx context.Context, accountID int64) ([]domain.LedgerEntry, error) {
	var exists bool
	_ = s.db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM accounts WHERE id=$1)", accountID).Scan(&exists)
	if !exists {
		return nil, ErrAccountNotFound
	}

	rows, err := s.db.Query(ctx, "SELECT account_id, delta FROM ledger_entries WHERE account_id = $1 ORDER BY created_at DESC", accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []domain.LedgerEntry
	for rows.Next() {
		var e domain.LedgerEntry
		if err := rows.Scan(&e.AccountID, &e.Delta); err == nil {
			entries = append(entries, e)
		}
	}
	return entries, nil
}

func (s *LedgerStore) ExecTransfer(ctx context.Context, req domain.TransferRequest, idempotencyKey, reqHash string) (*domain.TransferResponse, error) {
	// Start Tx with Repeatable Read
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// 1. Idempotency Check
	var storedStatus string
	var storedBody json.RawMessage
	var storedHash string

	err = tx.QueryRow(ctx, "SELECT status, response_body, request_hash FROM idempotency_keys WHERE key = $1", idempotencyKey).
		Scan(&storedStatus, &storedBody, &storedHash)

	if err == nil {
		if storedHash != reqHash {
			return nil, fmt.Errorf("idempotency key mismatch")
		}
		if storedStatus == "in_progress" {
			return nil, ErrConflict
		}
		var resp domain.TransferResponse
		if err := json.Unmarshal(storedBody, &resp); err != nil {
			return nil, err
		}
		return &resp, nil
	} else if err != pgx.ErrNoRows {
		return nil, err
	}

	// 2. Reserve Key
	_, err = tx.Exec(ctx, "INSERT INTO idempotency_keys (key, request_hash, status) VALUES ($1, $2, 'in_progress')", idempotencyKey, reqHash)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrConflict
		}
		return nil, err
	}

	// 3. Deterministic Locking (Smallest ID first)
	first, second := req.FromAccountID, req.ToAccountID
	if first > second {
		first, second = second, first
	}

	// UPDATED: Use NOWAIT to fail fast during contention
	for _, id := range []int64{first, second} {
		var b int64
		// We use NOWAIT here. If locked, Postgres returns code 55P03
		if err := tx.QueryRow(ctx, "SELECT balance FROM accounts WHERE id = $1 FOR UPDATE NOWAIT", id).Scan(&b); err != nil {
			var pgErr *pgconn.PgError
			// 55P03 is lock_not_available
			if errors.As(err, &pgErr) && pgErr.Code == "55P03" {
				return nil, ErrConflict
			}
			return nil, ErrAccountNotFound
		}
	}

	// 4. Check Balance
	var fromBalance int64
	if err := tx.QueryRow(ctx, "SELECT balance FROM accounts WHERE id = $1", req.FromAccountID).Scan(&fromBalance); err != nil {
		return nil, err
	}
	if fromBalance < req.Amount {
		return nil, fmt.Errorf("insufficient funds")
	}

	// 5. Execute Moves
	var transferID int64
	err = tx.QueryRow(ctx, "INSERT INTO transfers (from_account_id, to_account_id, amount, status) VALUES ($1, $2, $3, 'completed') RETURNING id",
		req.FromAccountID, req.ToAccountID, req.Amount).Scan(&transferID)
	if err != nil {
		return nil, err
	}

	_, err = tx.Exec(ctx, "INSERT INTO ledger_entries (transfer_id, account_id, delta) VALUES ($1, $2, $3), ($1, $4, $5)",
		transferID, req.FromAccountID, -req.Amount, req.ToAccountID, req.Amount)
	if err != nil {
		return nil, fmt.Errorf("ledger invariant likely violated: %v", err)
	}

	_, err = tx.Exec(ctx, "UPDATE accounts SET balance = balance - $1 WHERE id = $2", req.Amount, req.FromAccountID)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, "UPDATE accounts SET balance = balance + $1 WHERE id = $2", req.Amount, req.ToAccountID)
	if err != nil {
		return nil, err
	}

	// 6. Finalize
	resp := domain.TransferResponse{
		Transfer: domain.Transfer{ID: transferID, FromAccountID: req.FromAccountID, ToAccountID: req.ToAccountID, Amount: req.Amount, Status: "completed"},
		Entries:  []domain.LedgerEntry{{AccountID: req.FromAccountID, Delta: -req.Amount}, {AccountID: req.ToAccountID, Delta: req.Amount}},
	}

	respBytes, _ := json.Marshal(resp)
	_, err = tx.Exec(ctx, "UPDATE idempotency_keys SET status = 'completed', transfer_id = $1, response_status = 201, response_body = $2 WHERE key = $3",
		transferID, respBytes, idempotencyKey)
	if err != nil {
		return nil, err
	}

	return &resp, tx.Commit(ctx)
}
