package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/TemirlanKoybaev/tripgoavito.git/api"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/model"
)

func (h *Handler) FinishTrip(w http.ResponseWriter, r *http.Request, tripId api.TripId) {
	var trip *model.Trip

	err := h.txManager.Do(r.Context(), func(ctx context.Context) error {
		n, err := h.tripRepo.Finish(ctx, tripId, time.Now().UTC().Truncate(time.Microsecond))
		if err != nil {
			return err
		}

		if n == 0 {
			existing, err := h.tripRepo.GetByID(ctx, tripId)
			if err != nil {
				return err
			}
			if existing.Status == model.StatusCompleted {
				return model.ErrTripCompleted
			}
			return fmt.Errorf("trip %s has status %q but was not finished", tripId, existing.Status)
		}

		from := model.StatusActive
		if err := h.histRepo.Create(ctx, tripId, &from, model.StatusCompleted); err != nil {
			return err
		}

		trip, err = h.tripRepo.GetByID(ctx, tripId)
		return err
	})
	if err != nil {
		switch {
		case errors.Is(err, model.ErrTripNotFound):
			writeProblem(w, http.StatusNotFound, "trip_not_found", "Trip not found", r.URL.Path)
		case errors.Is(err, model.ErrTripCompleted):
			writeProblem(w, http.StatusConflict, "trip_completed", "Trip already completed", r.URL.Path)
		default:
			log.Printf("finish trip error: %v", err)
			writeProblem(w, http.StatusInternalServerError, "internal_error", "Internal server error", r.URL.Path)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(tripToResponse(trip))
}
