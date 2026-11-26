//go:build integration

package trip_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/infra/postgres/transaction"
	triprepo "github.com/TemirlanKoybaev/tripgoavito.git/internal/repository/trip"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/usecase/trip/finish"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/usecase/trip/positions"
)

type failingPositionUpdate struct {
	*triprepo.Repository
	err error
}

func (r failingPositionUpdate) UpdateLastPositionAt(ctx context.Context, id uuid.UUID, at time.Time) error {
	if err := r.Repository.UpdateLastPositionAt(ctx, id, at); err != nil {
		return err
	}
	return r.err
}

func TestPositionUseCaseRollsBackOnError(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	tr := e.newTrip(t)
	if err := e.trips.Create(ctx, tr); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	wantErr := errors.New("position update failed")
	u := positions.New(failingPositionUpdate{e.trips, wantErr}, e.tx, nil, nil, time.Now)
	got, err := u.Create(ctx, domain.Position{TripID: tr.ID, RecordedAt: tr.StartedAt})
	if !errors.Is(err, wantErr) || got != (domain.Position{}) {
		t.Fatalf("Create() = %+v, %v, want zero position and %v", got, err, wantErr)
	}
	assertEmptyRoute(t, e, tr)
	stored, err := e.trips.GetByID(ctx, tr.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if stored.LastPositionAt != nil {
		t.Errorf("last_position_at = %v after rollback, want nil", stored.LastPositionAt)
	}
}

type discardFinishMetrics struct{}

func (discardFinishMetrics) TripCompleted(context.Context) {}

func TestPositionWaitsForConcurrentFinish(t *testing.T) {
	e := newEnv(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	tr := e.newTrip(t)
	if err := e.trips.Create(ctx, tr); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	finishTrip := finish.New(e.trips, e.history, e.tx, discardFinishMetrics{}, slog.New(slog.DiscardHandler))
	savePosition := positions.New(e.trips, e.tx, nil, nil, time.Now)
	saved := make(chan error, 1)

	err := e.tx.Do(ctx, func(txCtx context.Context) error {
		if _, err := finishTrip.Finish(txCtx, tr.ID); err != nil {
			return err
		}
		var finishPID int32
		executor := transaction.GetExecutor(txCtx, e.pool)
		if err := executor.QueryRow(txCtx, "SELECT pg_backend_pid()").Scan(&finishPID); err != nil {
			return err
		}
		go func() {
			_, err := savePosition.Create(ctx, domain.Position{TripID: tr.ID, RecordedAt: tr.StartedAt})
			saved <- err
		}()
		// Удерживаем транзакцию finish до фактического ожидания блокировки в PostgreSQL.
		return waitForPositionLock(ctx, e.pool, finishPID, saved)
	})
	if err != nil {
		t.Fatalf("finish transaction: %v", err)
	}
	select {
	case err := <-saved:
		if !errors.Is(err, domain.ErrTripCompleted) {
			t.Fatalf("Create() error = %v, want %v", err, domain.ErrTripCompleted)
		}
	case <-ctx.Done():
		t.Fatal("position save did not finish after the lock was released")
	}
	assertEmptyRoute(t, e, tr)
	stored, err := e.trips.GetByID(ctx, tr.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if stored.Status != domain.StatusCompleted || stored.LastPositionAt != nil {
		t.Errorf("trip = %+v, want completed without a position", stored)
	}
}

func waitForPositionLock(ctx context.Context, pool *pgxpool.Pool, finishPID int32, saved <-chan error) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-saved:
			return fmt.Errorf("position save finished before finish committed: %v", err)
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			var blocked bool
			err := pool.QueryRow(ctx, `SELECT EXISTS (
    SELECT 1 FROM pg_stat_activity
    WHERE datname = current_database() AND wait_event_type = 'Lock'
      AND $1 = ANY(pg_blocking_pids(pid))
)`, finishPID).Scan(&blocked)
			if err != nil || blocked {
				return err
			}
		}
	}
}
