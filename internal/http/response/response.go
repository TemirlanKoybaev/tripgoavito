package response

import (
	"encoding/json"
	"net/http"

	"github.com/TemirlanKoybaev/tripgoavito.git/api"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
)

// WrittenError передаёт причину ошибки для ответа, чей статус уже отправлен.
type WrittenError struct {
	Status int
	Code   string
	Cause  error
}

func (e *WrittenError) Error() string { return e.Cause.Error() }

func (e *WrittenError) Unwrap() error { return e.Cause }

func JSON(w http.ResponseWriter, status int, body any) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		return &WrittenError{Status: status, Cause: err}
	}
	return nil
}

func Trip(t *trip.Trip) api.Trip {
	return api.Trip{
		Id:         t.ID,
		UserId:     t.UserID,
		DriverId:   t.DriverID,
		StartPoint: api.Coordinates{Latitude: t.StartLatitude, Longitude: t.StartLongitude},
		EndPoint:   api.Coordinates{Latitude: t.EndLatitude, Longitude: t.EndLongitude},
		Price:      t.Price,
		Status:     api.TripStatus(t.Status),
		StartedAt:  t.StartedAt,
		FinishedAt: t.FinishedAt,
	}
}
