package main

import (
	"log"
	"net/http"
	"os"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/punchamoorthee/ledgerops/internal/api"
	"github.com/punchamoorthee/ledgerops/internal/service"
	"github.com/punchamoorthee/ledgerops/internal/store"
)

func main() {
	// 1. Configuration
	connString := os.Getenv("DB_SOURCE")
	if connString == "" {
		log.Fatal("DB_SOURCE environment variable is required")
	}

	// 2. Database Initialization
	store, err := store.NewStore(connString)
	if err != nil {
		log.Fatalf("Database connection failure: %v\n", err)
	}
	defer store.Close()

	// 3. Service Initialization
	transferService := service.NewTransferService(store.Db)

	// 4. Handler Initialization
	h := api.NewHandler(store, transferService)

	// 5. Router Setup
	r := mux.NewRouter()

	// System endpoints
	r.HandleFunc("/healthz", h.HealthCheckHandler).Methods("GET")
	r.Handle("/metrics", promhttp.Handler()).Methods("GET")

	// Domain endpoints
	r.HandleFunc("/accounts", h.CreateAccountHandler).Methods("POST")
	r.HandleFunc("/transfers", h.CreateTransferHandler).Methods("POST")
	r.HandleFunc("/transfers/{id}", h.GetTransferHandler).Methods("GET")
	r.HandleFunc("/accounts/{id}", h.GetAccountHandler).Methods("GET")
	r.HandleFunc("/accounts/{id}/entries", h.GetAccountEntriesHandler).Methods("GET")

	// 6. Start Server
	log.Println("Service listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", r))
}
