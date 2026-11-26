package positions_test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/TemirlanKoybaev/tripgoavito.git/api"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/http/router"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/http/trip/positions"
	usecase "github.com/TemirlanKoybaev/tripgoavito.git/internal/usecase/trip/positions"
)

const validBody = `{"coordinates":{"latitude":0,"longitude":0},"recorded_at":"2026-11-27T00:00:00Z"}`

func TestPositionEndpoints(t *testing.T) {
	uc := NewMockusecase(gomock.NewController(t))
	id := uuid.New()
	input := trip.Position{TripID: id, RecordedAt: positionTime()}
	saved := input
	saved.ID = 42
	uc.EXPECT().Create(gomock.Any(), input).Return(saved, nil)
	uc.EXPECT().List(gomock.Any(), id).Return([]trip.Position{saved}, nil)
	uc.EXPECT().List(gomock.Any(), id).Return(nil, nil)
	handler := router.New(nil, nil, nil, positions.New(uc), nil, slog.New(slog.DiscardHandler)).Handler()
	path := positionsPath(id)
	want := api.TripPosition{Id: saved.ID, TripId: id, RecordedAt: input.RecordedAt}

	created := request(handler, http.MethodPost, path, validBody)
	var point api.TripPosition
	readJSON(t, created, http.StatusCreated, &point)
	if point != want {
		t.Errorf("created position = %+v, want %+v", point, want)
	}
	listed := request(handler, http.MethodGet, path, "")
	var route api.TripPositionsResponse
	readJSON(t, listed, http.StatusOK, &route)
	if len(route.Positions) != 1 || route.Positions[0] != want {
		t.Errorf("positions = %+v, want [%+v]", route.Positions, want)
	}
	empty := request(handler, http.MethodGet, path, "")
	readJSON(t, empty, http.StatusOK, &route)
	if route.Positions == nil || len(route.Positions) != 0 {
		t.Errorf("empty route = %s, want an empty array", empty.Body)
	}
}

func TestPositionRequestValidation(t *testing.T) {
	// Проверки формы и значений проходят через реальную связку обработчика и usecase.
	uc := usecase.New(nil, nil, nil, positionTime)
	handler := router.New(nil, nil, nil, positions.New(uc), nil, slog.New(slog.DiscardHandler)).Handler()
	path := positionsPath(uuid.New())
	tests := []struct {
		name string
		body string
	}{
		{"malformed JSON", `{"coordinates":`},
		{"missing coordinates", `{"recorded_at":"2026-11-27T00:00:00Z"}`},
		{"missing latitude", `{"coordinates":{"longitude":0},"recorded_at":"2026-11-27T00:00:00Z"}`},
		{"null longitude", strings.Replace(validBody, `"longitude":0`, `"longitude":null`, 1)},
		{"missing time", `{"coordinates":{"latitude":0,"longitude":0}}`},
		{"null time", `{"coordinates":{"latitude":0,"longitude":0},"recorded_at":null}`},
		{"invalid time", strings.Replace(validBody, "2026-11-27T00:00:00Z", "2026-11-27", 1)},
		{"time too far in future", strings.Replace(validBody, "00:00:00Z", "00:01:00.000000001Z", 1)},
		{"latitude out of range", strings.Replace(validBody, `"latitude":0`, `"latitude":91`, 1)},
		{"longitude out of range", strings.Replace(validBody, `"longitude":0`, `"longitude":-181`, 1)},
		{"wrong coordinate type", strings.Replace(validBody, `"longitude":0`, `"longitude":"0"`, 1)},
		{"unknown field", strings.Replace(validBody, `"recorded_at":`, `"extra":0,"recorded_at":`, 1)},
		{"unknown coordinates field", strings.Replace(validBody, `"latitude":`, `"extra":0,"latitude":`, 1)},
		{"trailing JSON", validBody + ` {}`},
		{"oversized body", validBody + strings.Repeat(" ", 1<<20)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := request(handler, http.MethodPost, path, tt.body)
			assertProblem(t, w, http.StatusBadRequest, "invalid_request")
		})
	}
}

func TestPositionErrorsUseProblemJSON(t *testing.T) {
	uc := NewMockusecase(gomock.NewController(t))
	id := uuid.New()
	input := trip.Position{TripID: id, RecordedAt: positionTime()}
	handler := router.New(nil, nil, nil, positions.New(uc), nil, slog.New(slog.DiscardHandler)).Handler()
	path := positionsPath(id)
	gomock.InOrder(
		uc.EXPECT().Create(gomock.Any(), input).Return(trip.Position{}, trip.ErrTripNotFound),
		uc.EXPECT().Create(gomock.Any(), input).Return(trip.Position{}, trip.ErrTripCompleted),
	)
	uc.EXPECT().List(gomock.Any(), id).Return(nil, trip.ErrTripNotFound)

	const notFound = "trip_not_found"
	assertProblem(t, request(handler, http.MethodPost, path, validBody), http.StatusNotFound, notFound)
	assertProblem(t, request(handler, http.MethodPost, path, validBody), http.StatusConflict, "trip_completed")
	assertProblem(t, request(handler, http.MethodGet, path, ""), http.StatusNotFound, notFound)
}

func positionTime() time.Time {
	return time.Date(2026, time.November, 27, 0, 0, 0, 0, time.UTC)
}

func positionsPath(id uuid.UUID) string {
	return "/api/v1/trips/" + id.String() + "/positions"
}

func request(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(w, r)
	return w
}

func readJSON(t *testing.T, w *httptest.ResponseRecorder, status int, dst any) {
	t.Helper()
	if ct := w.Header().Get("Content-Type"); w.Code != status || ct != "application/json" {
		t.Fatalf("response = %d %s %s, want %d application/json", w.Code, ct, w.Body, status)
	}
	if err := json.Unmarshal(w.Body.Bytes(), dst); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

func assertProblem(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if ct := w.Header().Get("Content-Type"); w.Code != status || ct != "application/problem+json" {
		t.Fatalf("response = %d %s %s, want %d application/problem+json", w.Code, ct, w.Body, status)
	}
	var problem api.Problem
	if err := json.Unmarshal(w.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if int(problem.Status) != status || problem.Code != code {
		t.Errorf("problem = %+v, want status %d and code %q", problem, status, code)
	}
}
