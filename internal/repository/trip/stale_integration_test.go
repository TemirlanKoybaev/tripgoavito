//go:build integration

package trip_test

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	domain "github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/infra/postgres/transaction"
)

func TestListStaleTripsByCursor(t *testing.T) {
	e := newEnv(t)
	cutoff := time.Date(2026, time.November, 27, 10, 0, 0, 0, time.UTC)
	oldest := cutoff.Add(-5 * time.Minute)
	tied := cutoff.Add(-3 * time.Minute)
	latest := cutoff.Add(-time.Minute)
	fresh := cutoff.Add(time.Second)
	fixtures := []struct {
		id         byte
		startedAt  time.Time
		lastAt     *time.Time
		finishedAt *time.Time
	}{
		{id: 4, startedAt: oldest, lastAt: &latest},
		{id: 8, startedAt: fresh},
		{id: 3, startedAt: oldest, lastAt: &tied},
		{id: 7, startedAt: oldest, lastAt: &cutoff},
		{id: 2, startedAt: tied},
		{id: 6, startedAt: oldest, finishedAt: &tied},
		{id: 9, startedAt: oldest},
		{id: 5, startedAt: oldest, lastAt: &fresh},
		{id: 1, startedAt: oldest, lastAt: &tied},
	}

	err := e.tx.Do(t.Context(), func(ctx context.Context) error {
		db := transaction.GetExecutor(ctx, e.pool)
		// Копия изолирует тест от поездок, уже существующих в общей базе.
		if _, err := db.Exec(ctx, "CREATE TEMP TABLE trips (LIKE trips INCLUDING ALL) ON COMMIT DROP"); err != nil {
			return fmt.Errorf("create test table: %w", err)
		}

		byID := make(map[byte]domain.StaleTrip, len(fixtures))
		for _, f := range fixtures {
			item := domain.StaleTrip{TripID: uuid.UUID{15: f.id}, DriverID: uuid.New(), EffectiveAt: f.startedAt}
			if f.lastAt != nil {
				item.EffectiveAt = *f.lastAt
			}
			status := domain.StatusActive
			if f.finishedAt != nil {
				status = domain.StatusCompleted
			}
			if _, err := db.Exec(ctx, `INSERT INTO trips (
    id, user_id, driver_id, start_latitude, start_longitude, end_latitude, end_longitude,
    price, status, started_at, finished_at, last_position_at
) VALUES ($1, $2, $3, 59.93, 30.31, 59.94, 30.32, 1200, $4, $5, $6, $7)`,
				item.TripID, uuid.New(), item.DriverID, status, f.startedAt, f.finishedAt, f.lastAt); err != nil {
				return fmt.Errorf("insert trip %d: %w", f.id, err)
			}
			byID[f.id] = item
		}

		var after *domain.StaleTripCursor
		for i, ids := range [][]byte{{9, 1}, {2, 3}, {4}, {}} {
			got, err := e.trips.ListStaleTrips(ctx, cutoff, after, 2)
			if err != nil {
				return fmt.Errorf("read page %d: %w", i+1, err)
			}
			want := make([]domain.StaleTrip, 0, len(ids))
			for _, id := range ids {
				want = append(want, byID[id])
			}
			if !slices.Equal(got, want) {
				return fmt.Errorf("page %d = %+v, want %+v", i+1, got, want)
			}
			if len(got) > 0 {
				last := got[len(got)-1]
				after = &domain.StaleTripCursor{EffectiveAt: last.EffectiveAt, TripID: last.TripID}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
