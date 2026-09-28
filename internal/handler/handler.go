package handler

import (
	"encoding/json"
	"net/http"

	"github.com/TemirlanKoybaev/tripgoavito.git/api"
)

type Handler struct{}

func New() *Handler {
	return &Handler{}
}

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(api.HealthResponse{Status: api.Ok})
}

func (h *Handler) Ready(w http.ResponseWriter, r *http.Request) {
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
