package domainerror

import (
	"errors"
	"net/http"
	"strings"

	"github.com/TemirlanKoybaev/tripgoavito.git/api"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
)

const problemBaseURL = "https://tripgo.example/problems/"

func Problem(err error) (int, api.Problem) {
	var validation *trip.ValidationError

	switch {
	case errors.Is(err, trip.ErrTripNotFound):
		return build(http.StatusNotFound, "trip_not_found", "Trip not found", "Trip not found")
	case errors.Is(err, trip.ErrTripCompleted):
		return build(http.StatusConflict, "trip_completed", "Trip completed", "Trip already completed")
	case errors.Is(err, trip.ErrDriverBusy):
		return build(http.StatusConflict, "driver_busy", "Driver busy", "Driver already has an active trip")
	case errors.As(err, &validation):
		return build(http.StatusBadRequest, "invalid_request", "Invalid request", validation.Message)
	default:
		return build(http.StatusInternalServerError, "internal_error", "Internal server error", "Internal server error")
	}
}

func build(status int32, code, title, detail string) (int, api.Problem) {
	return int(status), api.Problem{
		Type:   problemBaseURL + strings.ReplaceAll(code, "_", "-"),
		Title:  title,
		Status: status,
		Detail: &detail,
		Code:   code,
	}
}
