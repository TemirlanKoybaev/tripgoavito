package response

import (
	"encoding/json"
	"net/http"

	"github.com/TemirlanKoybaev/tripgoavito.git/api"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/http/domainerror"
)

func JSON(w http.ResponseWriter, r *http.Request, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		domainerror.Record(r.Context(), err)
	}
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
