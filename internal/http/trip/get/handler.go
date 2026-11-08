package get

import (
	"net/http"

	"github.com/TemirlanKoybaev/tripgoavito.git/api"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/http/domainerror"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/http/response"
)

type Handler struct {
	usecase usecase
}

func New(u usecase) *Handler {
	return &Handler{usecase: u}
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request, tripID api.TripId) {
	t, err := h.usecase.Get(r.Context(), tripID)
	if err != nil {
		domainerror.Write(w, r, err)
		return
	}

	response.JSON(w, r, http.StatusOK, response.Trip(t))
}
