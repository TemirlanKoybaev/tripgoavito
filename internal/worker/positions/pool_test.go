package positions_test

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/metric/noop"
	"go.uber.org/goleak"
	"go.uber.org/mock/gomock"

	"github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/infra/telemetry"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/worker/positions"
)

const testBudget = 3 * time.Second

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestPoolPrioritizesAndEvictsAcrossEnqueues(t *testing.T) {
	gate, freshest := candidate(9, 9), candidate(8, 8)
	older, firstTie, secondTie := candidate(1, 1), candidate(2, 4), candidate(3, 4)
	calls := make(chan uuid.UUID, 8)
	release := make(chan struct{})
	pool := newTestPool(t, 1, 3, testBudget, func(ctx context.Context, _, tripID uuid.UUID) error {
		calls <- tripID
		if tripID == gate.TripID {
			return waitForRelease(ctx, release)
		}
		if tripID == older.TripID {
			return errors.New("push service unavailable")
		}
		return nil
	})
	enqueue(t, pool, gate, positions.Queued)
	if got := receive(t, calls); got != gate.TripID {
		t.Fatalf("started trip = %s, want %s", got, gate.TripID)
	}
	for _, trip := range []trip.StaleTrip{freshest, secondTie, firstTie} {
		enqueue(t, pool, trip, positions.Queued)
	}
	result := pool.Enqueue(t.Context(), older)
	if result.Status != positions.Queued || result.Evicted == nil || *result.Evicted != freshest {
		t.Fatalf("enqueue older trip = %+v, want freshest trip evicted", result)
	}
	if pool.Cancel(t.Context(), freshest.TripID) {
		t.Fatal("evicted trip was still pending")
	}
	enqueue(t, pool, candidate(6, 10), positions.Dropped)
	enqueue(t, pool, candidate(5, 4), positions.Dropped)
	close(release)
	shutdown(t, pool)
	assertCalls(t, calls, older.TripID, firstTie.TripID, secondTie.TripID)
}

func TestPoolCancelsPendingAndDeduplicatesInFlight(t *testing.T) {
	gate, waiting, discarded := candidate(1, 1), candidate(2, 2), candidate(3, 3)
	started := make(chan uuid.UUID, 8)
	releaseGate := make(chan struct{})
	releaseWaiting := make(chan struct{})
	pool := newTestPool(t, 1, 2, testBudget, func(ctx context.Context, _, tripID uuid.UUID) error {
		started <- tripID
		switch tripID {
		case gate.TripID:
			return waitForRelease(ctx, releaseGate)
		case waiting.TripID:
			return waitForRelease(ctx, releaseWaiting)
		default:
			return nil
		}
	})
	enqueue(t, pool, gate, positions.Queued)
	if got := receive(t, started); got != gate.TripID {
		t.Fatalf("started trip = %s, want %s", got, gate.TripID)
	}
	enqueue(t, pool, gate, positions.Duplicate)
	if pool.Cancel(t.Context(), gate.TripID) {
		t.Fatal("cancel removed an in-flight task")
	}
	enqueue(t, pool, waiting, positions.Queued)
	enqueue(t, pool, waiting, positions.Duplicate)
	enqueue(t, pool, discarded, positions.Queued)
	if !pool.Cancel(t.Context(), waiting.TripID) || !pool.Cancel(t.Context(), discarded.TripID) || pool.Cancel(t.Context(), discarded.TripID) {
		t.Fatal("expected cancellation to remove each queued trip once")
	}
	waiting.EffectiveAt = waiting.EffectiveAt.Add(time.Minute)
	enqueue(t, pool, waiting, positions.Queued)
	close(releaseGate)
	if got := receive(t, started); got != waiting.TripID {
		t.Fatalf("started trip = %s, want %s", got, waiting.TripID)
	}
	enqueue(t, pool, waiting, positions.Duplicate)
	enqueue(t, pool, gate, positions.Queued)
	close(releaseWaiting)
	shutdown(t, pool)
	assertCalls(t, started, gate.TripID)
	enqueue(t, pool, candidate(4, 4), positions.Dropped)
}

func TestPoolStartsOnlyConfiguredWorkersAndDrains(t *testing.T) {
	var active atomic.Int32
	calls := make(chan uuid.UUID, 6)
	release := make(chan struct{})
	pool := newTestPool(t, 2, 4, testBudget, func(ctx context.Context, _, tripID uuid.UUID) error {
		if running := active.Add(1); running > 2 {
			t.Errorf("%d concurrent requests, want at most 2", running)
		}
		defer active.Add(-1)
		calls <- tripID
		return waitForRelease(ctx, release)
	})
	for id := byte(1); id <= 4; id++ {
		enqueue(t, pool, candidate(id, int(id)), positions.Queued)
	}
	receive(t, calls)
	receive(t, calls)
	if running := active.Load(); running != 2 {
		t.Fatalf("active requests = %d, want 2", running)
	}
	close(release)
	shutdown(t, pool)
	if got := len(calls); got != 2 {
		t.Errorf("drained requests = %d, want 2", got)
	}
}

func TestPoolTimesOutTaskAndProcessesNext(t *testing.T) {
	first, second := candidate(1, 1), candidate(2, 2)
	started := make(chan uuid.UUID, 2)
	result := make(chan error, 1)
	pool := newTestPool(t, 1, 2, 30*time.Millisecond, func(ctx context.Context, _, tripID uuid.UUID) error {
		started <- tripID
		if tripID == second.TripID {
			return nil
		}
		<-ctx.Done()
		result <- ctx.Err()
		return ctx.Err()
	})
	enqueue(t, pool, first, positions.Queued)
	enqueue(t, pool, second, positions.Queued)
	shutdown(t, pool)
	if err := receive(t, result); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("first request = %v, want deadline exceeded", err)
	}
	assertCalls(t, started, first.TripID, second.TripID)
}

func TestPoolShutdownDeadlineCancelsRequestAndDropsWaiting(t *testing.T) {
	started := make(chan uuid.UUID, 2)
	result := make(chan error, 2)
	pool := newTestPool(t, 1, 2, testBudget, func(ctx context.Context, _, tripID uuid.UUID) error {
		started <- tripID
		<-ctx.Done()
		result <- ctx.Err()
		return ctx.Err()
	})
	first := candidate(1, 1)
	enqueue(t, pool, first, positions.Queued)
	if got := receive(t, started); got != first.TripID {
		t.Fatalf("started trip = %s, want %s", got, first.TripID)
	}
	enqueue(t, pool, candidate(2, 2), positions.Queued)
	expired, cancel := context.WithDeadline(t.Context(), time.Unix(0, 0))
	defer cancel()
	if err := pool.Shutdown(expired); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown() = %v, want deadline exceeded", err)
	}
	shutdown(t, pool)
	if err := receive(t, result); !errors.Is(err, context.Canceled) {
		t.Errorf("in-flight request = %v, want cancelled", err)
	}
	assertCalls(t, started)
}

func newTestPool(t *testing.T, workers, capacity int, timeout time.Duration, send func(context.Context, uuid.UUID, uuid.UUID) error) *positions.Pool {
	t.Helper()
	metrics, err := telemetry.NewMetrics(noop.NewMeterProvider().Meter("test"))
	if err != nil {
		t.Fatal(err)
	}
	return startTestPool(t, workers, capacity, timeout, metrics, send)
}

func startTestPool(t *testing.T, workers, capacity int, timeout time.Duration, metrics *telemetry.Metrics, send func(context.Context, uuid.UUID, uuid.UUID) error) *positions.Pool {
	t.Helper()
	gateway := NewMockpushGateway(gomock.NewController(t))
	gateway.EXPECT().RequestPosition(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(send).AnyTimes()
	pool, err := positions.NewPool(t.Context(), gateway, metrics, workers, capacity, timeout, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(func() { shutdown(t, pool) })
	return pool
}

func candidate(id byte, age int) trip.StaleTrip {
	return trip.StaleTrip{TripID: uuid.UUID{15: id}, DriverID: uuid.UUID{0: 1, 15: id}, EffectiveAt: time.Unix(int64(age), 0)}
}

func enqueue(t *testing.T, pool *positions.Pool, trip trip.StaleTrip, want positions.EnqueueStatus) {
	t.Helper()
	if got := pool.Enqueue(t.Context(), trip); got.Status != want || got.Evicted != nil {
		t.Fatalf("Enqueue(%s) = %+v, want %s with no eviction", trip.TripID, got, want)
	}
}

func waitForRelease(ctx context.Context, release <-chan struct{}) error {
	select {
	case <-release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func receive[T any](t *testing.T, values <-chan T) T {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), testBudget)
	defer cancel()
	select {
	case value := <-values:
		return value
	case <-ctx.Done():
		t.Fatal("timed out waiting for worker")
		var zero T
		return zero
	}
}

func shutdown(t *testing.T, pool *positions.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), testBudget)
	defer cancel()
	if err := pool.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

func assertCalls(t *testing.T, calls chan uuid.UUID, want ...uuid.UUID) {
	t.Helper()
	close(calls)
	got := make([]uuid.UUID, 0, len(calls))
	for id := range calls {
		got = append(got, id)
	}
	if !slices.Equal(got, want) {
		t.Errorf("requested trips = %v, want %v", got, want)
	}
}
