package main

import (
	"context"
	"log"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/punchamoorthee/ledgerops/internal/api"
	"github.com/punchamoorthee/ledgerops/internal/config"
	"github.com/punchamoorthee/ledgerops/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	dbPool, err := pgxpool.New(context.Background(), cfg.DBSource)
	if err != nil {
		log.Fatalf("Unable to connect to database: %v", err)
	}
	defer dbPool.Close()

	// Initialize Layers
	ledgerStore := store.NewLedgerStore(dbPool)
	handler := api.NewHandler(ledgerStore)

	// Router
	r := mux.NewRouter()
	r.Handle("/metrics", promhttp.Handler())
	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	apiV1 := r.PathPrefix("/api/v1").Subrouter()
	apiV1.HandleFunc("/accounts", handler.CreateAccount).Methods("POST")
	apiV1.HandleFunc("/accounts/{id}", handler.GetAccount).Methods("GET")
	apiV1.HandleFunc("/transfers", handler.CreateTransfer).Methods("POST")

	log.Printf("Server starting on :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, r); err != nil {
		log.Fatal(err)
	}
}
