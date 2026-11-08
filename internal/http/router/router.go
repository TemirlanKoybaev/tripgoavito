package router

import (
	"net/http"

	"github.com/TemirlanKoybaev/tripgoavito.git/api"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/http/domainerror"
)

type (
	createHandler interface {
		Handle(w http.ResponseWriter, r *http.Request, params api.CreateTripParams)
	}

	getHandler interface {
		Handle(w http.ResponseWriter, r *http.Request, tripID api.TripId)
	}

	finishHandler interface {
		Handle(w http.ResponseWriter, r *http.Request, tripID api.TripId)
	}

	healthHandler interface {
		Health(w http.ResponseWriter, r *http.Request)
		Ready(w http.ResponseWriter, r *http.Request)
	}
)

type Router struct {
	create createHandler
	get    getHandler
	finish finishHandler
	health healthHandler
}

func New(create createHandler, get getHandler, finish finishHandler, health healthHandler) *Router {
	return &Router{create: create, get: get, finish: finish, health: health}
}

func (s *Router) CreateTrip(w http.ResponseWriter, r *http.Request, params api.CreateTripParams) {
	s.create.Handle(w, r, params)
}

func (s *Router) GetTrip(w http.ResponseWriter, r *http.Request, tripID api.TripId) {
	s.get.Handle(w, r, tripID)
}

func (s *Router) FinishTrip(w http.ResponseWriter, r *http.Request, tripID api.TripId) {
	s.finish.Handle(w, r, tripID)
}

func (s *Router) Health(w http.ResponseWriter, r *http.Request) {
	s.health.Health(w, r)
}

func (s *Router) Ready(w http.ResponseWriter, r *http.Request) {
	s.health.Ready(w, r)
}

func (s *Router) Handler() http.Handler {
	return api.HandlerWithOptions(s, api.ChiServerOptions{
		ErrorHandlerFunc: paramErrorHandler,
	})
}

func paramErrorHandler(w http.ResponseWriter, r *http.Request, err error) {
	domainerror.Write(w, r, &trip.ValidationError{Message: err.Error()})
}
