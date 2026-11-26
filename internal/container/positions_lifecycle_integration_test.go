//go:build integration

package container

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/TemirlanKoybaev/tripgoavito.git/api"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/config"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
	triprepo "github.com/TemirlanKoybaev/tripgoavito.git/internal/repository/trip"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/worker/positions"
)

const lifecycleBudget = 3 * time.Second

func TestShutdownDrainsQueueAfterHTTPCommitsPosition(t *testing.T) {
	gate, retained := uuid.New(), uuid.New()
	requests := make(chan uuid.UUID, 4)
	releasePush := make(chan struct{})
	cfg := integrationConfig(t, func(w http.ResponseWriter, r *http.Request) {
		id := pushedTripID(t, r)
		requests <- id
		if id == gate {
			select {
			case <-releasePush:
			case <-r.Context().Done():
			}
		}
		w.WriteHeader(http.StatusAccepted)
	})
	cfg.PositionWorkers = 1
	cfg.PositionQueueSize = 3
	cfg.PushRequestTimeout = 5 * time.Second
	var logs bytes.Buffer
	c := startIntegrationContainer(t, cfg, slog.New(slog.NewTextHandler(&logs, nil)))
	recordedAt := time.Now().UTC().Truncate(time.Microsecond)
	target := createLifecycleTrip(t, c, cfg, uuid.New(), recordedAt)
	path := "/api/v1/trips/" + target.ID.String() + "/positions"
	enteredHTTP, releaseHTTP := make(chan struct{}), make(chan struct{})
	next := c.Server.Handler
	c.Server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(enteredHTTP)
		select {
		case <-releaseHTTP:
			next.ServeHTTP(w, r)
		case <-r.Context().Done():
		case <-t.Context().Done():
		}
	})
	stopping := make(chan struct{}, 1)
	c.Server.RegisterOnShutdown(func() {
		select {
		case stopping <- struct{}{}:
		default:
		}
	})
	server := httptest.NewUnstartedServer(c.Server.Handler)
	server.Config = c.Server
	server.Start()
	t.Cleanup(server.Close)
	queueTrip(t, c, gate, uuid.New(), recordedAt)
	if got := waitLifecycle(t, requests); got != gate {
		t.Fatalf("first Push trip = %s, want %s", got, gate)
	}
	queueTrip(t, c, target.ID, target.DriverID, recordedAt)
	queueTrip(t, c, retained, uuid.New(), recordedAt)

	postDone := make(chan error, 1)
	go func() { postDone <- postPosition(t.Context(), server, path, recordedAt) }()
	waitLifecycle(t, enteredHTTP)
	ctx, cancel := context.WithTimeout(t.Context(), lifecycleBudget)
	defer cancel()
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- c.Shutdown(ctx) }()
	waitLifecycle(t, stopping)
	if got := c.workers.Enqueue(t.Context(), trip.StaleTrip{TripID: retained}); got.Status != positions.Duplicate {
		t.Fatalf("queue closed before the HTTP request finished: %+v", got)
	}
	close(releaseHTTP)
	if err := waitLifecycle(t, postDone); err != nil {
		t.Fatal(err)
	}
	if queued, _ := c.workers.Stats(); queued != 1 {
		t.Errorf("queue size after committed coordinate = %d, want 1", queued)
	}
	close(releasePush)
	if err := waitLifecycle(t, shutdownDone); err != nil {
		t.Fatal(err)
	}
	if got := waitLifecycle(t, requests); got != retained || len(requests) != 0 {
		t.Errorf("remaining Push trip = %s, extra requests = %d, want only %s", got, len(requests), retained)
	}
	assertShutdownOrder(t, logs.String())
}

func TestShutdownDeadlineCancelsHungPush(t *testing.T) {
	started := make(chan uuid.UUID, 2)
	stopped := make(chan struct{}, 2)
	cfg := integrationConfig(t, func(_ http.ResponseWriter, r *http.Request) {
		started <- pushedTripID(t, r)
		<-r.Context().Done()
		stopped <- struct{}{}
	})
	cfg.PositionWorkers = 1
	cfg.PositionQueueSize = 2
	cfg.PushRequestTimeout = 5 * time.Second
	c := startIntegrationContainer(t, cfg, slog.New(slog.DiscardHandler))
	first := uuid.New()
	queueTrip(t, c, first, uuid.New(), time.Now())
	if got := waitLifecycle(t, started); got != first {
		t.Fatalf("started trip = %s, want %s", got, first)
	}
	queueTrip(t, c, uuid.New(), uuid.New(), time.Now())
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if err := c.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown() = %v, want deadline exceeded", err)
	}
	waitLifecycle(t, stopped)
	waitLifecycle(t, c.released)
	if got := len(started); got != 0 {
		t.Errorf("requests started after the deadline = %d, want 0", got)
	}
}

func TestRestartScansTripsAlreadyInPostgres(t *testing.T) {
	id := uuid.New()
	requested := make(chan uuid.UUID, 2)
	cfg := integrationConfig(t, func(w http.ResponseWriter, r *http.Request) {
		if pushed := pushedTripID(t, r); pushed == id {
			select {
			case requested <- pushed:
			default:
			}
		}
		w.WriteHeader(http.StatusAccepted)
	})
	cfg.PositionCheckInterval = 50 * time.Millisecond
	cfg.PositionStaleAfter = 24 * time.Hour
	logger := slog.New(slog.DiscardHandler)
	c := startIntegrationContainer(t, cfg, logger)
	createLifecycleTrip(t, c, cfg, id, time.Date(1970, time.January, 1, 0, 0, 0, 0, time.UTC))
	waitLifecycle(t, requested)
	shutdownContainer(t, c)
	for len(requested) != 0 {
		<-requested
	}
	startIntegrationContainer(t, cfg, logger)
	waitLifecycle(t, requested)
}

func createLifecycleTrip(t *testing.T, c *Container, cfg *config.Config, id uuid.UUID, at time.Time) *trip.Trip {
	t.Helper()
	target := &trip.Trip{
		ID: id, UserID: uuid.New(), DriverID: uuid.New(), Status: trip.StatusActive, StartedAt: at,
		StartLatitude: 59.93, StartLongitude: 30.33, EndLatitude: 59.94, EndLongitude: 30.34,
	}
	if err := triprepo.New(c.pool, cfg.DatabaseQueryTimeout).Create(t.Context(), target); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), lifecycleBudget)
		defer cancel()
		conn, err := pgx.Connect(ctx, cfg.DatabaseURL)
		if err != nil {
			t.Errorf("clean up trip: %v", err)
			return
		}
		defer func() { _ = conn.Close(ctx) }()
		if _, err := conn.Exec(ctx, "DELETE FROM trips WHERE id = $1", target.ID); err != nil {
			t.Errorf("clean up trip: %v", err)
		}
	})
	return target
}

func queueTrip(t *testing.T, c *Container, tripID, driverID uuid.UUID, at time.Time) {
	t.Helper()
	result := c.workers.Enqueue(t.Context(), trip.StaleTrip{TripID: tripID, DriverID: driverID, EffectiveAt: at})
	if result.Status != positions.Queued || result.Evicted != nil {
		t.Fatalf("Enqueue(%s) = %+v, want queued without eviction", tripID, result)
	}
}

func pushedTripID(t *testing.T, r *http.Request) uuid.UUID {
	t.Helper()
	var body struct {
		Data struct {
			TripID uuid.UUID `json:"trip_id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Errorf("decode Push request: %v", err)
	}
	if _, err := io.Copy(io.Discard, r.Body); err != nil {
		t.Errorf("read Push request: %v", err)
	}
	return body.Data.TripID
}

func postPosition(ctx context.Context, server *httptest.Server, path string, at time.Time) error {
	body, err := json.Marshal(api.PositionData{Coordinates: api.Coordinates{Latitude: 59.93, Longitude: 30.33}, RecordedAt: at})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := server.Client().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("POST positions status = %d, want 201", resp.StatusCode)
	}
	return nil
}

func assertShutdownOrder(t *testing.T, log string) {
	t.Helper()
	httpStopped := strings.Index(log, "http server stopped")
	scannerStopped := strings.Index(log, "position scanner stopped")
	workersStopped := strings.Index(log, "position workers stopped")
	if httpStopped < 0 || scannerStopped <= httpStopped || workersStopped <= scannerStopped {
		t.Errorf("wrong shutdown order in log:\n%s", log)
	}
}

func waitLifecycle[T any](t *testing.T, events <-chan T) T {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), lifecycleBudget)
	defer cancel()
	select {
	case event := <-events:
		return event
	case <-ctx.Done():
		t.Fatal("timed out waiting for lifecycle event")
		var zero T
		return zero
	}
}
