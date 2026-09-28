package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/TemirlanKoybaev/tripgoavito.git/api"
	"github.com/jackc/pgx/v5"
)

func (h *Handler) GetTrip(w http.ResponseWriter, r *http.Request, tripId api.TripId) {
	trip, err := h.tripRepo.GetByID(r.Context(), tripId)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeProblem(w, http.StatusNotFound, "trip_not_found", "Trip not found", r.URL.Path)
			return
		}
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Internal server error", r.URL.Path)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(tripToResponse(trip))
}
