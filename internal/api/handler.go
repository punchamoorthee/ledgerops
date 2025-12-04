package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/punchamoorthee/ledgerops/internal/domain"
	"github.com/punchamoorthee/ledgerops/internal/store"
)

// Prometheus Metrics
var (
	httpReqTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ledger_http_requests_total",
		Help: "Total HTTP requests classified by status",
	}, []string{"method", "endpoint", "status"})

	httpLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ledger_http_request_duration_seconds",
		Help:    "Request latency distribution",
		Buckets: []float64{0.005, 0.01, 0.05, 0.1, 0.5, 1},
	}, []string{"method", "endpoint"})
)

type Handler struct {
	store *store.LedgerStore
}

func NewHandler(s *store.LedgerStore) *Handler {
	return &Handler{store: s}
}

func (h *Handler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	timer := prometheus.NewTimer(httpLatency.WithLabelValues("POST", "/transfers"))
	defer timer.ObserveDuration()

	idemKey := r.Header.Get("Idempotency-Key")
	if idemKey == "" {
		h.respondError(w, http.StatusBadRequest, "Missing Idempotency-Key header", "POST", "/transfers")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "Failed to read body", "POST", "/transfers")
		return
	}

	// Create Hash for Idempotency check
	hash := sha256.Sum256(body)
	reqHash := hex.EncodeToString(hash[:])

	// Re-populate body for decoder
	r.Body = io.NopCloser(bytes.NewBuffer(body))

	var req domain.TransferRequest
	if err := json.Unmarshal(body, &req); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid JSON", "POST", "/transfers")
		return
	}

	if req.Amount <= 0 {
		h.respondError(w, http.StatusUnprocessableEntity, "Amount must be positive", "POST", "/transfers")
		return
	}
	if req.FromAccountID == req.ToAccountID {
		h.respondError(w, http.StatusUnprocessableEntity, "Cannot transfer to self", "POST", "/transfers")
		return
	}

	resp, err := h.store.ExecTransfer(r.Context(), req, idemKey, reqHash)
	if err != nil {
		switch err {
		case store.ErrConflict:
			h.respondError(w, http.StatusConflict, "Request in progress or lock contention", "POST", "/transfers")
		case store.ErrAccountNotFound:
			h.respondError(w, http.StatusNotFound, "Account not found", "POST", "/transfers")
		case store.ErrKeyMismatch:
			h.respondError(w, http.StatusUnprocessableEntity, "Idempotency key reused with different payload", "POST", "/transfers")
		case store.ErrFunds:
			h.respondError(w, http.StatusUnprocessableEntity, "Insufficient funds", "POST", "/transfers")
		default:
			h.respondError(w, http.StatusInternalServerError, err.Error(), "POST", "/transfers")
		}
		return
	}

	w.Header().Set("Location", fmt.Sprintf("/transfers/%d", resp.Transfer.ID))
	// In a real scenario, we might return 200 for replays and 201 for creations,
	// but the payload handles the differentiation.
	h.respondJSON(w, http.StatusCreated, resp, "POST", "/transfers")
}

func (h *Handler) CreateAccount(w http.ResponseWriter, r *http.Request) {
	type req struct {
		InitialBalance int64 `json:"initial_balance"`
	}
	var p req
	json.NewDecoder(r.Body).Decode(&p)

	id, err := h.store.CreateAccount(r.Context(), p.InitialBalance)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, err.Error(), "POST", "/accounts")
		return
	}
	h.respondJSON(w, http.StatusCreated, map[string]int64{"id": id}, "POST", "/accounts")
}

func (h *Handler) GetAccount(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, _ := strconv.ParseInt(vars["id"], 10, 64)

	acc, err := h.store.GetAccount(r.Context(), id)
	if err != nil {
		if err == store.ErrAccountNotFound {
			h.respondError(w, http.StatusNotFound, "Account not found", "GET", "/accounts")
			return
		}
		h.respondError(w, http.StatusInternalServerError, err.Error(), "GET", "/accounts")
		return
	}
	h.respondJSON(w, http.StatusOK, acc, "GET", "/accounts")
}

func (h *Handler) respondJSON(w http.ResponseWriter, code int, payload interface{}, method, endpoint string) {
	httpReqTotal.WithLabelValues(method, endpoint, strconv.Itoa(code)).Inc()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(payload)
}

func (h *Handler) respondError(w http.ResponseWriter, code int, msg, method, endpoint string) {
	h.respondJSON(w, code, map[string]string{"error": msg}, method, endpoint)
}
