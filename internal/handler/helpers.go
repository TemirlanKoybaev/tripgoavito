package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/TemirlanKoybaev/tripgoavito.git/api"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/model"
)

var problemTitles = map[string]string{
	"invalid_request": "Invalid request",
	"trip_not_found":  "Trip not found",
	"trip_completed":  "Trip completed",
	"driver_busy":     "Driver busy",
	"internal_error":  "Internal server error",
}

func writeProblem(w http.ResponseWriter, status int, code, detail, instance string) {
	title, ok := problemTitles[code]
	if !ok {
		title = http.StatusText(status)
	}

	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(api.Problem{
		Type:     "https://tripgo.example/problems/" + strings.ReplaceAll(code, "_", "-"),
		Title:    title,
		Status:   int32(status),
		Detail:   &detail,
		Instance: &instance,
		Code:     code,
	})
}

func ParamErrorHandler(w http.ResponseWriter, r *http.Request, err error) {
	writeProblem(w, http.StatusBadRequest, "invalid_request", err.Error(), r.URL.Path)
}

func tripToResponse(trip *model.Trip) api.Trip {
	return api.Trip{
		Id:         trip.ID,
		UserId:     trip.UserID,
		DriverId:   trip.DriverID,
		StartPoint: api.Coordinates{Latitude: trip.StartLatitude, Longitude: trip.StartLongitude},
		EndPoint:   api.Coordinates{Latitude: trip.EndLatitude, Longitude: trip.EndLongitude},
		Price:      trip.Price,
		Status:     api.TripStatus(trip.Status),
		StartedAt:  trip.StartedAt,
		FinishedAt: trip.FinishedAt,
	}
}
