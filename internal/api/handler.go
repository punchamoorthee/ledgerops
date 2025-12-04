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

// Metrics
var (
	httpReqTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ledger_http_requests_total",
		Help: "Total HTTP requests",
	}, []string{"method", "endpoint", "status"})

	httpLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ledger_http_request_duration_seconds",
		Help:    "Request latency",
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
		h.respondError(w, http.StatusBadRequest, "Missing Idempotency-Key", "POST", "/transfers")
		return
	}

	body, _ := io.ReadAll(r.Body)
	hash := sha256.Sum256(body)
	reqHash := hex.EncodeToString(hash[:])
	r.Body = io.NopCloser(bytes.NewBuffer(body))

	var req domain.TransferRequest
	if err := json.Unmarshal(body, &req); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid JSON", "POST", "/transfers")
		return
	}

	// Validation
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
		if err == store.ErrConflict {
			h.respondError(w, http.StatusConflict, "Request in progress", "POST", "/transfers")
			return
		}
		if err == store.ErrAccountNotFound {
			h.respondError(w, http.StatusNotFound, "Account not found", "POST", "/transfers")
			return
		}
		if err.Error() == "idempotency key mismatch" {
			h.respondError(w, http.StatusUnprocessableEntity, "Key reuse mismatch", "POST", "/transfers")
			return
		}
		if err.Error() == "insufficient funds" {
			h.respondError(w, http.StatusUnprocessableEntity, "Insufficient funds", "POST", "/transfers")
			return
		}
		h.respondError(w, http.StatusInternalServerError, err.Error(), "POST", "/transfers")
		return
	}

	// Detect if this was a new creation or a replay. In a real system we'd check the store result details.
	// For simplicity, we assume 201, but the client handles 200/201 identically usually.
	w.Header().Set("Location", fmt.Sprintf("/transfers/%d", resp.Transfer.ID))
	h.respondJSON(w, http.StatusCreated, resp, "POST", "/transfers")
}

func (h *Handler) CreateAccount(w http.ResponseWriter, r *http.Request) {
	id, err := h.store.CreateAccount(r.Context(), 0)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, err.Error(), "POST", "/accounts")
		return
	}
	h.respondJSON(w, http.StatusCreated, map[string]int64{"id": id}, "POST", "/accounts")
}

func (h *Handler) GetAccount(w http.ResponseWriter, r *http.Request) {
	idStr := mux.Vars(r)["id"]
	id, _ := strconv.ParseInt(idStr, 10, 64)

	acc, err := h.store.GetAccount(r.Context(), id)
	if err != nil {
		if err == store.ErrAccountNotFound {
			h.respondError(w, http.StatusNotFound, "Not Found", "GET", "/accounts/{id}")
			return
		}
		h.respondError(w, http.StatusInternalServerError, err.Error(), "GET", "/accounts/{id}")
		return
	}
	h.respondJSON(w, http.StatusOK, acc, "GET", "/accounts/{id}")
}

// Helpers
func (h *Handler) respondJSON(w http.ResponseWriter, code int, payload interface{}, method, endpoint string) {
	httpReqTotal.WithLabelValues(method, endpoint, strconv.Itoa(code)).Inc()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(payload)
}

func (h *Handler) respondError(w http.ResponseWriter, code int, msg, method, endpoint string) {
	h.respondJSON(w, code, map[string]string{"error": msg}, method, endpoint)
}
