//go:build integration

package container

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/goleak"

	"github.com/TemirlanKoybaev/tripgoavito.git/api"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/config"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func newIntegrationContainer(t *testing.T) (*Container, time.Duration) {
	t.Helper()
	cfg := integrationConfig(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	return startIntegrationContainer(t, cfg, slog.New(slog.DiscardHandler)), cfg.DatabaseQueryTimeout
}

func integrationConfig(t *testing.T, pushHandler http.HandlerFunc) *config.Config {
	t.Helper()
	push := httptest.NewServer(pushHandler)
	t.Cleanup(push.Close)
	t.Setenv("PUSH_HTTP_URL", push.URL)
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(collector.Close)
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", collector.URL+"/v1/traces")
	t.Setenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", collector.URL+"/v1/metrics")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.PositionCheckInterval = time.Hour
	return cfg
}

func startIntegrationContainer(t *testing.T, cfg *config.Config, lg *slog.Logger) *Container {
	t.Helper()
	c, err := New(context.WithoutCancel(t.Context()), cfg, lg)
	if err != nil {
		t.Fatalf("build service: %v", err)
	}
	t.Cleanup(func() { shutdownContainer(t, c) })
	return c
}

func shutdownContainer(t *testing.T, c *Container) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 3*time.Second)
	defer cancel()
	if err := c.Shutdown(ctx); err != nil {
		t.Errorf("shutdown: %v", err)
	}
}

func TestCreateTripDriverBusy(t *testing.T) {
	c, queryTimeout := newIntegrationContainer(t)
	server := httptest.NewServer(c.Server.Handler)
	t.Cleanup(server.Close)

	driverID := uuid.New()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
		defer cancel()
		if _, err := c.pool.Exec(ctx, "DELETE FROM trips WHERE driver_id = $1", driverID); err != nil {
			t.Errorf("clean up trips: %v", err)
		}
	})

	body, err := json.Marshal(api.TripData{
		UserId:     uuid.New(),
		DriverId:   driverID,
		StartPoint: api.Coordinates{Latitude: 59.9398, Longitude: 30.3146},
		EndPoint:   api.Coordinates{Latitude: 59.929, Longitude: 30.3626},
		Price:      1450,
	})
	if err != nil {
		t.Fatalf("encode trip: %v", err)
	}

	client := server.Client()
	client.Timeout = 5 * time.Second
	post := func(wantStatus int) *http.Response {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+"/api/v1/trips", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("create HTTP request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("post trip: %v", err)
		}
		t.Cleanup(func() { _ = resp.Body.Close() })
		if resp.StatusCode != wantStatus {
			got, err := io.ReadAll(resp.Body)
			t.Fatalf("status = %d, want %d; body = %s (read error: %v)", resp.StatusCode, wantStatus, got, err)
		}
		return resp
	}

	post(http.StatusCreated)
	conflict := post(http.StatusConflict)
	if got := conflict.Header.Get("Content-Type"); got != "application/problem+json" {
		t.Errorf("content type = %q, want application/problem+json", got)
	}
	var problem api.Problem
	if err := json.NewDecoder(conflict.Body).Decode(&problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if problem.Status != http.StatusConflict || problem.Code != "driver_busy" {
		t.Errorf("problem = %+v, want status 409 and code driver_busy", problem)
	}
}

func TestSaveAndListTripPositions(t *testing.T) {
	c, queryTimeout := newIntegrationContainer(t)
	handler := c.Server.Handler
	const tripsPath = "/api/v1/trips"
	created := requestJSON[api.Trip](t, handler, http.MethodPost, tripsPath, api.TripData{
		UserId:     uuid.New(),
		DriverId:   uuid.New(),
		StartPoint: api.Coordinates{Latitude: 59.93, Longitude: 30.31},
		EndPoint:   api.Coordinates{Latitude: 59.94, Longitude: 30.32},
		Price:      1450,
	}, http.StatusCreated)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
		defer cancel()
		if _, err := c.pool.Exec(ctx, "DELETE FROM trips WHERE id = $1", created.Id); err != nil {
			t.Errorf("clean up trip: %v", err)
		}
	})

	tripPath := tripsPath + "/" + created.Id.String()
	positionsPath := tripPath + "/positions"
	now := time.Now().UTC().Truncate(time.Microsecond)
	post := func(at time.Time, latitude float64) api.TripPosition {
		t.Helper()
		return requestJSON[api.TripPosition](t, handler, http.MethodPost, positionsPath, api.PositionData{
			Coordinates: api.Coordinates{Latitude: latitude, Longitude: 30.31},
			RecordedAt:  at,
		}, http.StatusCreated)
	}
	first := post(now.Add(789*time.Nanosecond), 59.93)
	second := post(now, 59.94)
	late := post(now.Add(-time.Minute), 59.95)
	if first.RecordedAt != now || first.TripId != created.Id || first.Id <= 0 {
		t.Errorf("first point = %+v, want persisted position at %v for trip %s", first, now, created.Id)
	}
	stored := requestJSON[api.Trip](t, handler, http.MethodGet, tripPath, nil, http.StatusOK)
	if stored.LastPositionAt == nil || *stored.LastPositionAt != now {
		t.Errorf("last_position_at = %v, want %v after the late point", stored.LastPositionAt, now)
	}

	requestJSON[api.Trip](t, handler, http.MethodPost, tripPath+"/finish", nil, http.StatusOK)
	route := requestJSON[api.TripPositionsResponse](t, handler, http.MethodGet, positionsPath, nil, http.StatusOK)
	want := []api.TripPosition{late, first, second}
	if !slices.Equal(route.Positions, want) {
		t.Errorf("positions = %+v, want %+v", route.Positions, want)
	}
}

func requestJSON[T any](t *testing.T, handler http.Handler, method, path string, body any, status int) T {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encode request: %v", err)
	}
	req := httptest.NewRequestWithContext(t.Context(), method, path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != status {
		t.Fatalf("%s %s status = %d, want %d, body = %s", method, path, w.Code, status, w.Body)
	}
	var response T
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return response
}
