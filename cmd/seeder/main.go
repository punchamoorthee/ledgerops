package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	TotalAccounts  = 1000
	InitialBalance = 10000 // $100.00
)

func main() {
	dbURL := os.Getenv("DB_SOURCE")
	if dbURL == "" {
		// Fallback for local development if env not set
		dbURL = "postgresql://admin:secret@localhost:5433/ledger?sslmode=disable"
	}

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		log.Fatalf("Unable to connect to database: %v\n", err)
	}
	defer conn.Close(ctx)

	log.Println("--- Seeding Database ---")

	// 1. Clean Slate (Optional: dangerous in prod, useful for bench)
	// _, err = conn.Exec(ctx, "TRUNCATE TABLE accounts, transfers, ledger_entries, idempotency_keys CASCADE")
	// if err != nil { log.Fatal(err) }

	// 2. Check existing
	var count int
	conn.QueryRow(ctx, "SELECT COUNT(*) FROM accounts").Scan(&count)
	if count >= TotalAccounts {
		log.Printf("Database already has %d accounts. Skipping.", count)
		return
	}

	// 3. Bulk Insert using CopyFrom (Fastest method)
	log.Printf("Generating %d accounts...", TotalAccounts)
	rows := [][]interface{}{}
	for i := 0; i < TotalAccounts; i++ {
		rows = append(rows, []interface{}{int64(InitialBalance), time.Now()})
	}

	copyCount, err := conn.CopyFrom(
		ctx,
		pgx.Identifier{"accounts"},
		[]string{"balance", "created_at"},
		pgx.CopyFromRows(rows),
	)

	if err != nil {
		log.Fatalf("Bulk insert failed: %v", err)
	}

	log.Printf("Successfully seeded %d accounts.", copyCount)
}
