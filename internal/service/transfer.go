package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/punchamoorthee/ledgerops/internal/models"
)

var (
	ErrAccountNotFound     = errors.New("account not found")
	ErrInsufficientFunds   = errors.New("insufficient funds")
	ErrIdempotencyConflict = errors.New("request in progress")
	ErrIdempotencyMismatch = errors.New("key reuse with mismatched payload")
)

type TransferService struct {
	db *pgxpool.Pool
}

func NewTransferService(db *pgxpool.Pool) *TransferService {
	return &TransferService{db: db}
}

// ProcessTransfer executes the double-entry transfer within a transaction with deterministic locking.
func (s *TransferService) ProcessTransfer(ctx context.Context, req models.TransferRequest, idempotencyKey string, reqHash string) (*models.TransferResponse, *models.IdempotencyRecord, error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, nil, fmt.Errorf("tx begin failed: %w", err)
	}
	defer tx.Rollback(ctx)

	// 1. Idempotency Check
	var storedStatus int
	var storedBody json.RawMessage
	var storedHash string
	err = tx.QueryRow(ctx,
		"SELECT response_status, response_body, request_hash FROM idempotency_keys WHERE key = $1",
		idempotencyKey,
	).Scan(&storedStatus, &storedBody, &storedHash)

	if err == nil {
		// Key exists
		if storedHash != reqHash {
			return nil, nil, ErrIdempotencyMismatch
		}
		return nil, &models.IdempotencyRecord{
			Key:            idempotencyKey,
			Status:         "completed", // effectively completed if we have a body
			ResponseBody:   storedBody,
			ResponseStatus: storedStatus,
		}, nil
	} else if err != pgx.ErrNoRows {
		return nil, nil, fmt.Errorf("idempotency query failed: %w", err)
	}

	// 2. Idempotency Reservation
	_, err = tx.Exec(ctx,
		"INSERT INTO idempotency_keys (key, request_hash, status) VALUES ($1, $2, 'in_progress')",
		idempotencyKey, reqHash,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, nil, ErrIdempotencyConflict
		}
		return nil, nil, fmt.Errorf("key reservation failed: %w", err)
	}

	// 3. Deterministic Locking (Deadlock Prevention)
	acc1_id, acc2_id := req.FromAccountID, req.ToAccountID
	if acc1_id > acc2_id {
		acc1_id, acc2_id = req.ToAccountID, req.FromAccountID
	}

	var balance1, balance2 int64
	// Acquire locks in ID order
	err = tx.QueryRow(ctx, "SELECT balance FROM accounts WHERE id = $1 FOR UPDATE", acc1_id).Scan(&balance1)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil, ErrAccountNotFound
		}
		return nil, nil, fmt.Errorf("lock acquisition failed: %w", err)
	}
	err = tx.QueryRow(ctx, "SELECT balance FROM accounts WHERE id = $1 FOR UPDATE", acc2_id).Scan(&balance2)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil, ErrAccountNotFound
		}
		return nil, nil, fmt.Errorf("lock acquisition failed: %w", err)
	}

	// 4. Business Logic Check
	var fromBalance int64
	if req.FromAccountID == acc1_id {
		fromBalance = balance1
	} else {
		fromBalance = balance2
	}

	if fromBalance < req.Amount {
		return nil, nil, ErrInsufficientFunds
	}

	// 5. Execution: Insert Transfer & Ledger Entries
	var transferID int64
	err = tx.QueryRow(ctx,
		"INSERT INTO transfers (from_account_id, to_account_id, amount, status) VALUES ($1, $2, $3, 'completed') RETURNING id",
		req.FromAccountID, req.ToAccountID, req.Amount,
	).Scan(&transferID)
	if err != nil {
		return nil, nil, fmt.Errorf("transfer insert failed: %w", err)
	}

	// Batch insert ledger entries (Debit and Credit)
	_, err = tx.Exec(ctx,
		"INSERT INTO ledger_entries (transfer_id, account_id, delta) VALUES ($1, $2, $3), ($1, $4, $5)",
		transferID, req.FromAccountID, -req.Amount, req.ToAccountID, req.Amount,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("ledger entry failed: %w", err)
	}

	// 6. Update Balances
	_, err = tx.Exec(ctx, "UPDATE accounts SET balance = balance - $1 WHERE id = $2", req.Amount, req.FromAccountID)
	if err != nil {
		return nil, nil, err
	}
	_, err = tx.Exec(ctx, "UPDATE accounts SET balance = balance + $1 WHERE id = $2", req.Amount, req.ToAccountID)
	if err != nil {
		return nil, nil, err
	}

	// 7. Finalize Idempotency & Commit
	resp := &models.TransferResponse{
		Transfer: models.Transfer{
			ID:            transferID,
			FromAccountID: req.FromAccountID,
			ToAccountID:   req.ToAccountID,
			Amount:        req.Amount,
			Status:        "completed",
		},
		Entries: []models.LedgerEntry{
			{AccountID: req.FromAccountID, Delta: -req.Amount},
			{AccountID: req.ToAccountID, Delta: req.Amount},
		},
	}

	respBody, err := json.Marshal(resp)
	if err != nil {
		return nil, nil, err
	}

	_, err = tx.Exec(ctx,
		"UPDATE idempotency_keys SET status = 'completed', transfer_id = $1, response_status = $2, response_body = $3 WHERE key = $4",
		transferID, http.StatusCreated, respBody, idempotencyKey,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("idempotency update failed: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf("tx commit failed: %w", err)
	}

	return resp, nil, nil
}
