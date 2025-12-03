package main

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	TotalAccounts  = 1000
	InitialBalance = 1000 // In cents
)

func main() {
	// Uses localhost because it runs on the host machine via the script
	connString := "postgres://admin:secret@localhost:5432/ledger?sslmode=disable"

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, connString)
	if err != nil {
		log.Fatalf("Seeder connection failed: %v", err)
	}
	defer conn.Close(ctx)

	log.Println("--- Seeding Database ---")

	var count int
	conn.QueryRow(ctx, "SELECT COUNT(*) FROM accounts").Scan(&count)
	if count > 0 {
		log.Printf("Database already contains %d accounts. Skipping.", count)
		return
	}

	// Optimized Bulk Insert (Copy Protocol)
	log.Printf("Generating %d accounts...", TotalAccounts)
	rows := [][]interface{}{}
	for i := 0; i < TotalAccounts; i++ {
		// CHANGE: We removed the 'nil' for ID.
		// We now only provide Balance and CreatedAt.
		rows = append(rows, []interface{}{int64(InitialBalance), time.Now()})
	}

	countCopy, err := conn.CopyFrom(
		ctx,
		pgx.Identifier{"accounts"},
		// CHANGE: We removed "id" from this list. Postgres will auto-generate it.
		[]string{"balance", "created_at"},
		pgx.CopyFromRows(rows),
	)

	if err != nil {
		log.Fatalf("Bulk insert failed: %v", err)
	}

	log.Printf("Successfully seeded %d accounts.", countCopy)
}
