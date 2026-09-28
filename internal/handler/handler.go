package handler

import (
	"encoding/json"
	"net/http"

	"github.com/TemirlanKoybaev/tripgoavito.git/api"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Handler {
	return &Handler{pool: pool}
}

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(api.HealthResponse{Status: api.Ok})
}

func (h *Handler) Ready(w http.ResponseWriter, r *http.Request) {
	if err := h.pool.Ping(r.Context()); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(api.HealthResponse{Status: api.Unavailable})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(api.HealthResponse{Status: api.Ok})
}

func (h *Handler) CreateTrip(w http.ResponseWriter, r *http.Request, params api.CreateTripParams) {
	w.WriteHeader(http.StatusNotImplemented)
}

func (h *Handler) GetTrip(w http.ResponseWriter, r *http.Request, tripId api.TripId) {
	w.WriteHeader(http.StatusNotImplemented)
}

func (h *Handler) FinishTrip(w http.ResponseWriter, r *http.Request, tripId api.TripId) {
	w.WriteHeader(http.StatusNotImplemented)
}

func (h *Handler) ListTripPositions(w http.ResponseWriter, r *http.Request, tripId api.TripId) {
	w.WriteHeader(http.StatusNotImplemented)
}

func (h *Handler) CreateTripPosition(w http.ResponseWriter, r *http.Request, tripId api.TripId) {
	w.WriteHeader(http.StatusNotImplemented)
}
