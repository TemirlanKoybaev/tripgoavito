package create

import (
	"net/http"

	"github.com/TemirlanKoybaev/tripgoavito.git/api"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/http/domainerror"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/http/response"
	createuc "github.com/TemirlanKoybaev/tripgoavito.git/internal/usecase/trip/create"
)

type Handler struct {
	usecase usecase
}

func New(u usecase) *Handler {
	return &Handler{usecase: u}
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request, _ api.CreateTripParams) {
	body, err := decode(w, r)
	if err != nil {
		domainerror.Write(w, r, err)
		return
	}

	t, err := h.usecase.Create(r.Context(), createuc.Input{
		UserID:         body.UserId,
		DriverID:       body.DriverId,
		StartLatitude:  body.StartPoint.Latitude,
		StartLongitude: body.StartPoint.Longitude,
		EndLatitude:    body.EndPoint.Latitude,
		EndLongitude:   body.EndPoint.Longitude,
		Price:          body.Price,
	})
	if err != nil {
		domainerror.Write(w, r, err)
		return
	}

	w.Header().Set("Location", "/api/v1/trips/"+t.ID.String())
	response.JSON(w, http.StatusCreated, response.Trip(t))
}
