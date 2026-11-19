package router

import (
	"log/slog"
	"net/http"

	"github.com/TemirlanKoybaev/tripgoavito.git/api"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/http/middleware"
)

type (
	createHandler interface {
		Handle(w http.ResponseWriter, r *http.Request, params api.CreateTripParams) error
	}

	getHandler interface {
		Handle(w http.ResponseWriter, r *http.Request, tripID api.TripId) error
	}

	finishHandler interface {
		Handle(w http.ResponseWriter, r *http.Request, tripID api.TripId) error
	}

	healthHandler interface {
		Health(w http.ResponseWriter, r *http.Request) error
		Ready(w http.ResponseWriter, r *http.Request) error
	}
)

type Router struct {
	create createHandler
	get    getHandler
	finish finishHandler
	health healthHandler
	log    *slog.Logger
}

var _ api.ServerInterface = (*Router)(nil)

func New(create createHandler, get getHandler, finish finishHandler, health healthHandler, log *slog.Logger) *Router {
	return &Router{create: create, get: get, finish: finish, health: health, log: log}
}

func (s *Router) CreateTrip(w http.ResponseWriter, r *http.Request, params api.CreateTripParams) {
	middleware.RespondError(s.log, w, r, s.create.Handle(w, r, params))
}

func (s *Router) GetTrip(w http.ResponseWriter, r *http.Request, tripID api.TripId) {
	middleware.RespondError(s.log, w, r, s.get.Handle(w, r, tripID))
}

func (s *Router) FinishTrip(w http.ResponseWriter, r *http.Request, tripID api.TripId) {
	middleware.RespondError(s.log, w, r, s.finish.Handle(w, r, tripID))
}

func (s *Router) Health(w http.ResponseWriter, r *http.Request) {
	middleware.RespondError(s.log, w, r, s.health.Health(w, r))
}

func (s *Router) Ready(w http.ResponseWriter, r *http.Request) {
	middleware.RespondError(s.log, w, r, s.health.Ready(w, r))
}

func (s *Router) Handler() http.Handler {
	return api.HandlerWithOptions(s, api.ChiServerOptions{
		Middlewares:      []api.MiddlewareFunc{middleware.TagRoute},
		ErrorHandlerFunc: s.paramErrorHandler,
	})
}

func (s *Router) paramErrorHandler(w http.ResponseWriter, r *http.Request, err error) {
	middleware.RespondError(s.log, w, r, &trip.ValidationError{Message: err.Error()})
}
