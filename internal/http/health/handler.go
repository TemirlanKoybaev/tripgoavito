package health

import (
	"context"
	"net/http"
	"time"

	"github.com/TemirlanKoybaev/tripgoavito.git/api"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/http/response"
)

type pinger interface {
	Ping(ctx context.Context) error
}

type Handler struct {
	pinger  pinger
	timeout time.Duration
}

func New(p pinger, timeout time.Duration) *Handler {
	return &Handler{pinger: p, timeout: timeout}
}

func (h *Handler) Health(w http.ResponseWriter, _ *http.Request) {
	response.JSON(w, http.StatusOK, api.HealthResponse{Status: api.Ok})
}

func (h *Handler) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()

	if err := h.pinger.Ping(ctx); err != nil {
		response.JSON(w, http.StatusServiceUnavailable, api.HealthResponse{Status: api.Unavailable})
		return
	}
	response.JSON(w, http.StatusOK, api.HealthResponse{Status: api.Ok})
}
