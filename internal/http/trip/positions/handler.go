package positions

import (
	"net/http"

	"github.com/TemirlanKoybaev/tripgoavito.git/api"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/http/response"
)

type Handler struct {
	usecase usecase
}

func New(u usecase) *Handler {
	return &Handler{usecase: u}
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request, tripID api.TripId) error {
	body, err := decode(w, r)
	if err != nil {
		return err
	}

	position, err := h.usecase.Create(r.Context(), trip.Position{
		TripID:     tripID,
		Latitude:   body.Coordinates.Latitude,
		Longitude:  body.Coordinates.Longitude,
		RecordedAt: body.RecordedAt,
	})
	if err != nil {
		return err
	}
	return response.JSON(w, http.StatusCreated, toAPI(position))
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request, tripID api.TripId) error {
	positions, err := h.usecase.List(r.Context(), tripID)
	if err != nil {
		return err
	}

	result := api.TripPositionsResponse{Positions: make([]api.TripPosition, 0, len(positions))}
	for _, position := range positions {
		result.Positions = append(result.Positions, toAPI(position))
	}
	return response.JSON(w, http.StatusOK, result)
}

func toAPI(p trip.Position) api.TripPosition {
	return api.TripPosition{
		Id:          p.ID,
		TripId:      p.TripID,
		Coordinates: api.Coordinates{Latitude: p.Latitude, Longitude: p.Longitude},
		RecordedAt:  p.RecordedAt,
	}
}
