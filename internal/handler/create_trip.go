package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/TemirlanKoybaev/tripgoavito.git/api"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/model"
	"github.com/google/uuid"
)

func (h *Handler) CreateTrip(w http.ResponseWriter, r *http.Request, params api.CreateTripParams) {
	body, err := decodeTripData(w, r)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_request", err.Error(), r.URL.Path)
		return
	}

	trip := &model.Trip{
		ID:             uuid.New(),
		UserID:         body.UserId,
		DriverID:       body.DriverId,
		StartLatitude:  body.StartPoint.Latitude,
		StartLongitude: body.StartPoint.Longitude,
		EndLatitude:    body.EndPoint.Latitude,
		EndLongitude:   body.EndPoint.Longitude,
		Price:          body.Price,
		Status:         model.StatusActive,
		StartedAt:      time.Now().UTC().Truncate(time.Microsecond),
	}

	err = h.txManager.Do(r.Context(), func(ctx context.Context) error {
		if err := h.tripRepo.Create(ctx, trip); err != nil {
			return err
		}
		return h.histRepo.Create(ctx, trip.ID, nil, model.StatusActive)
	})
	if err != nil {
		if errors.Is(err, model.ErrDriverBusy) {
			writeProblem(w, http.StatusConflict, "driver_busy", "Driver already has an active trip", r.URL.Path)
			return
		}
		log.Printf("create trip error: %v", err)
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Internal server error", r.URL.Path)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Location", "/api/v1/trips/"+trip.ID.String())
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(tripToResponse(trip))
}
