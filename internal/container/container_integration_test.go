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
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/TemirlanKoybaev/tripgoavito.git/api"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/config"
)

func TestCreateTripDriverBusy(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(collector.Close)
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", collector.URL+"/v1/traces")
	t.Setenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", collector.URL+"/v1/metrics")

	c, err := New(t.Context(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("build service: %v", err)
	}
	t.Cleanup(c.Close)
	server := httptest.NewServer(c.Server.Handler)
	t.Cleanup(server.Close)

	driverID := uuid.New()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), cfg.DatabaseQueryTimeout)
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
