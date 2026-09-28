package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/TemirlanKoybaev/tripgoavito.git/api"
	"github.com/jackc/pgx/v5"
)

func (h *Handler) FinishTrip(w http.ResponseWriter, r *http.Request, tripId api.TripId) {
	var rowsAffected int64

	err := h.txManager.Do(r.Context(), func(ctx context.Context) error {
		n, err := h.tripRepo.Finish(ctx, tripId, time.Now())
		if err != nil {
			return err
		}
		rowsAffected = n
		return nil
	})

	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Internal server error", r.URL.Path)
		return
	}

	if rowsAffected == 0 {
		trip, err := h.tripRepo.GetByID(r.Context(), tripId)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeProblem(w, http.StatusNotFound, "trip_not_found", "Trip not found", r.URL.Path)
				return
			}
			writeProblem(w, http.StatusInternalServerError, "internal_error", "Internal server error", r.URL.Path)
			return
		}
		if trip.Status == "completed" {
			writeProblem(w, http.StatusConflict, "trip_completed", "Trip already completed", r.URL.Path)
			return
		}
	}

	trip, err := h.tripRepo.GetByID(r.Context(), tripId)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Internal server error", r.URL.Path)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(tripToResponse(trip))
}
