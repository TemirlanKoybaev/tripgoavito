package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/TemirlanKoybaev/tripgoavito.git/api"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/repository"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/tx"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Pinger interface {
	Ping(ctx context.Context) error
}

type Handler struct {
	pinger       Pinger
	queryTimeout time.Duration
	tripRepo     *repository.TripRepository
	histRepo     *repository.StatusHistoryRepository
	txManager    *tx.TxManager
}

func New(pool *pgxpool.Pool, queryTimeout time.Duration) *Handler {
	return &Handler{
		pinger:       pool,
		queryTimeout: queryTimeout,
		tripRepo:     repository.NewTripRepository(pool, queryTimeout),
		histRepo:     repository.NewStatusHistoryRepository(pool, queryTimeout),
		txManager:    tx.New(pool),
	}
}

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(api.HealthResponse{Status: api.Ok})
}

func (h *Handler) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), h.queryTimeout)
	defer cancel()

	if err := h.pinger.Ping(ctx); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(api.HealthResponse{Status: api.Unavailable})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(api.HealthResponse{Status: api.Ok})
}

func (h *Handler) ListTripPositions(w http.ResponseWriter, r *http.Request, tripId api.TripId) {
	w.WriteHeader(http.StatusNotImplemented)
}

func (h *Handler) CreateTripPosition(w http.ResponseWriter, r *http.Request, tripId api.TripId) {
	w.WriteHeader(http.StatusNotImplemented)
}
