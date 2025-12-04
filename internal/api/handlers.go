package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/punchamoorthee/ledgerops/internal/models"
	"github.com/punchamoorthee/ledgerops/internal/service"
	"github.com/punchamoorthee/ledgerops/internal/store"
)

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

type Handler struct {
	store   *store.Store
	service *service.TransferService
}

func NewHandler(s *store.Store, svc *service.TransferService) *Handler {
	return &Handler{store: s, service: svc}
}

func (h *Handler) HealthCheckHandler(w http.ResponseWriter, r *http.Request) {
	respondWithJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) CreateAccountHandler(w http.ResponseWriter, r *http.Request) {
	id, err := h.store.CreateAccount(r.Context())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "System error creating account")
		return
	}
	respondWithJSON(w, http.StatusCreated, map[string]int64{"account_id": id})
}

func (h *Handler) CreateTransferHandler(w http.ResponseWriter, r *http.Request) {
	timer := prometheus.NewTimer(httpRequestDuration.WithLabelValues("POST", "/transfers"))
	defer timer.ObserveDuration()

	// 1. Validate Header
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		httpRequestsTotal.WithLabelValues("POST", "/transfers", "400").Inc()
		respondWithError(w, http.StatusBadRequest, "Missing Idempotency-Key header")
		return
	}

	// 2. Read and Hash Body
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		httpRequestsTotal.WithLabelValues("POST", "/transfers", "500").Inc()
		respondWithError(w, http.StatusInternalServerError, "Stream read error")
		return
	}
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	hash := sha256.Sum256(bodyBytes)
	reqHash := hex.EncodeToString(hash[:])

	var req models.TransferRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		httpRequestsTotal.WithLabelValues("POST", "/transfers", "400").Inc()
		respondWithError(w, http.StatusBadRequest, "Malformed JSON body")
		return
	}

	// 3. Business Validations
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

	// 4. Call Service
	resp, existing, err := h.service.ProcessTransfer(r.Context(), req, idempotencyKey, reqHash)

	// Handle Service Errors
	if err != nil {
		switch err {
		case service.ErrIdempotencyConflict:
			httpRequestsTotal.WithLabelValues("POST", "/transfers", "409").Inc()
			respondWithError(w, http.StatusConflict, "Request processing in progress")
		case service.ErrIdempotencyMismatch:
			httpRequestsTotal.WithLabelValues("POST", "/transfers", "422").Inc()
			respondWithError(w, http.StatusUnprocessableEntity, "Key reuse with mismatched payload")
		case service.ErrAccountNotFound:
			httpRequestsTotal.WithLabelValues("POST", "/transfers", "404").Inc()
			respondWithError(w, http.StatusNotFound, "Account not found")
		case service.ErrInsufficientFunds:
			httpRequestsTotal.WithLabelValues("POST", "/transfers", "422").Inc()
			respondWithError(w, http.StatusUnprocessableEntity, "Insufficient funds")
		default:
			httpRequestsTotal.WithLabelValues("POST", "/transfers", "500").Inc()
			respondWithError(w, http.StatusInternalServerError, "Internal Server Error")
		}
		return
	}

	// Handle Idempotent Replay
	if existing != nil {
		httpRequestsTotal.WithLabelValues("POST", "/transfers", "200").Inc()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(existing.ResponseStatus)
		w.Write(existing.ResponseBody)
		return
	}

	// Handle New Success
	httpRequestsTotal.WithLabelValues("POST", "/transfers", "201").Inc()
	w.Header().Set("Location", fmt.Sprintf("/transfers/%d", resp.Transfer.ID))
	respondWithJSON(w, http.StatusCreated, resp)
}

func (h *Handler) GetTransferHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	// Simple integer parsing could go here, omitting for brevity
	var id int64
	fmt.Sscanf(vars["id"], "%d", &id)

	transfer, err := h.store.GetTransfer(r.Context(), id)
	if err != nil {
		httpRequestsTotal.WithLabelValues("GET", "/transfers/{id}", "404").Inc()
		respondWithError(w, http.StatusNotFound, "Transfer not found")
		return
	}

	httpRequestsTotal.WithLabelValues("GET", "/transfers/{id}", "200").Inc()
	respondWithJSON(w, http.StatusOK, transfer)
}

func (h *Handler) GetAccountHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	var id int64
	fmt.Sscanf(vars["id"], "%d", &id)

	account, err := h.store.GetAccount(r.Context(), id)
	if err != nil {
		httpRequestsTotal.WithLabelValues("GET", "/accounts/{id}", "404").Inc()
		respondWithError(w, http.StatusNotFound, "Account not found")
		return
	}

	httpRequestsTotal.WithLabelValues("GET", "/accounts/{id}", "200").Inc()
	respondWithJSON(w, http.StatusOK, account)
}

func (h *Handler) GetAccountEntriesHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	var id int64
	fmt.Sscanf(vars["id"], "%d", &id)

	entries, err := h.store.GetEntries(r.Context(), id)
	if err != nil {
		httpRequestsTotal.WithLabelValues("GET", "/accounts/{id}/entries", "404").Inc()
		respondWithError(w, http.StatusNotFound, err.Error())
		return
	}

	httpRequestsTotal.WithLabelValues("GET", "/accounts/{id}/entries", "200").Inc()
	respondWithJSON(w, http.StatusOK, entries)
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
