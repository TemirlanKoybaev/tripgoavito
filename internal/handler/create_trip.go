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
	"github.com/jackc/pgx/v5/pgconn"
)

func (h *Handler) CreateTrip(w http.ResponseWriter, r *http.Request, params api.CreateTripParams) {
	var body api.TripData
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_request", "Invalid request body", r.URL.Path)
		return
	}

	if body.UserId == uuid.Nil || body.DriverId == uuid.Nil {
		writeProblem(w, http.StatusBadRequest, "invalid_request", "user_id and driver_id are required", r.URL.Path)
		return
	}

	if body.Price < 0 {
		writeProblem(w, http.StatusBadRequest, "invalid_request", "price must be >= 0", r.URL.Path)
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
		Status:         "active",
		StartedAt:      time.Now(),
	}

	if err := h.txManager.Do(r.Context(), func(ctx context.Context) error {
		if err := h.tripRepo.Create(ctx, trip); err != nil {
			return err
		}
		return h.histRepo.Create(ctx, trip.ID, nil, "active")
	}); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
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
