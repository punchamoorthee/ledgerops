package store

import (
	"context"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/punchamoorthee/ledgerops/internal/models"
)

type Store struct {
	Db *pgxpool.Pool
}

func NewStore(connString string) (*Store, error) {
	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("unable to parse database config: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		return nil, fmt.Errorf("unable to create connection pool: %w", err)
	}

	if err := pool.Ping(context.Background()); err != nil {
		return nil, fmt.Errorf("unable to ping database: %w", err)
	}

	return &Store{Db: pool}, nil
}

func (s *Store) Close() {
	s.Db.Close()
}

// GetAccount retrieves a single account by ID.
func (s *Store) GetAccount(ctx context.Context, id int64) (*models.Account, error) {
	var account models.Account
	err := s.Db.QueryRow(ctx, "SELECT id, balance FROM accounts WHERE id = $1", id).Scan(&account.ID, &account.Balance)
	if err != nil {
		return nil, err
	}
	return &account, nil
}

// CreateAccount creates a new account with 0 balance.
func (s *Store) CreateAccount(ctx context.Context) (int64, error) {
	var id int64
	err := s.Db.QueryRow(ctx, "INSERT INTO accounts (balance) VALUES (0) RETURNING id").Scan(&id)
	return id, err
}

// GetTransfer retrieves transfer details.
func (s *Store) GetTransfer(ctx context.Context, id int64) (*models.Transfer, error) {
	var t models.Transfer
	err := s.Db.QueryRow(ctx,
		"SELECT id, from_account_id, to_account_id, amount, status FROM transfers WHERE id = $1",
		id).Scan(&t.ID, &t.FromAccountID, &t.ToAccountID, &t.Amount, &t.Status)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// GetEntries retrieves ledger entries for a specific account.
func (s *Store) GetEntries(ctx context.Context, accountID int64) ([]models.LedgerEntry, error) {
	// First check if account exists
	var exists bool
	err := s.Db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM accounts WHERE id=$1)", accountID).Scan(&exists)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("account not found")
	}

	rows, err := s.Db.Query(ctx,
		"SELECT account_id, delta FROM ledger_entries WHERE account_id = $1 ORDER BY created_at DESC",
		accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []models.LedgerEntry
	for rows.Next() {
		var entry models.LedgerEntry
		if err := rows.Scan(&entry.AccountID, &entry.Delta); err != nil {
			log.Printf("Error scanning entry: %v", err)
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}
