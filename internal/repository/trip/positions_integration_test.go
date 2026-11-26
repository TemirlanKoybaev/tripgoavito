//go:build integration

package trip_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	domain "github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
)

func TestStorePositions(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	tr := e.newTrip(t)
	if err := e.trips.Create(ctx, tr); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	stored, err := e.trips.GetByID(ctx, tr.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if stored.LastPositionAt != nil {
		t.Fatalf("last_position_at = %v, want nil", stored.LastPositionAt)
	}
	assertEmptyRoute(t, e, tr)

	firstAt := tr.StartedAt.Add(time.Second)
	latestAt := firstAt.Add(time.Second)
	times := []time.Time{firstAt, latestAt, latestAt, tr.StartedAt}
	wantLast := []time.Time{firstAt, latestAt, latestAt, latestAt}
	saved := make([]domain.Position, 0, len(times))
	for i, at := range times {
		position := domain.Position{
			TripID:     tr.ID,
			Latitude:   59.9 + float64(i)/100,
			Longitude:  30.3 + float64(i)/100,
			RecordedAt: at.In(time.FixedZone("MSK", 3*60*60)),
		}
		var point domain.Position
		err := e.tx.Do(ctx, func(ctx context.Context) error {
			var err error
			point, err = e.trips.InsertPosition(ctx, position)
			if err != nil {
				return err
			}
			return e.trips.UpdateLastPositionAt(ctx, tr.ID, point.RecordedAt)
		})
		if err != nil {
			t.Fatalf("save position %d: %v", i, err)
		}
		if point.ID <= 0 || point.TripID != tr.ID || point.RecordedAt != at ||
			point.Latitude != position.Latitude || point.Longitude != position.Longitude {
			t.Fatalf("saved position = %+v, want %+v with generated id and UTC time", point, position)
		}
		saved = append(saved, point)

		updated, err := e.trips.GetByID(ctx, tr.ID)
		if err != nil {
			t.Fatalf("GetByID() error = %v", err)
		}
		if updated.LastPositionAt == nil || *updated.LastPositionAt != wantLast[i] {
			t.Fatalf("last_position_at after position %d = %v, want %v", i, updated.LastPositionAt, wantLast[i])
		}
	}

	got, err := e.trips.ListPositions(ctx, tr.ID)
	if err != nil {
		t.Fatalf("ListPositions() error = %v", err)
	}
	want := []domain.Position{saved[3], saved[0], saved[1], saved[2]}
	if !slices.Equal(got, want) {
		t.Errorf("route = %+v, want %+v", got, want)
	}

	other := e.newTrip(t)
	if err := e.trips.Create(ctx, other); err != nil {
		t.Fatalf("Create(other) error = %v", err)
	}
	assertEmptyRoute(t, e, other)
}

func TestPositionRepositoryRollback(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	tr := e.newTrip(t)
	if err := e.trips.Create(ctx, tr); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	rollback := errors.New("roll back position")

	err := e.tx.Do(ctx, func(ctx context.Context) error {
		position, err := e.trips.InsertPosition(ctx, domain.Position{
			TripID:     tr.ID,
			Latitude:   59.93,
			Longitude:  30.33,
			RecordedAt: tr.StartedAt,
		})
		if err != nil {
			return err
		}
		if err := e.trips.UpdateLastPositionAt(ctx, tr.ID, position.RecordedAt); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("Do() error = %v, want %v", err, rollback)
	}

	assertEmptyRoute(t, e, tr)
	stored, err := e.trips.GetByID(ctx, tr.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if stored.LastPositionAt != nil {
		t.Errorf("last_position_at after rollback = %v, want nil", stored.LastPositionAt)
	}
}

func assertEmptyRoute(t *testing.T, e *env, tr *domain.Trip) {
	t.Helper()

	positions, err := e.trips.ListPositions(context.Background(), tr.ID)
	if err != nil {
		t.Fatalf("ListPositions() error = %v", err)
	}
	if positions == nil || len(positions) != 0 {
		t.Fatalf("route = %#v, want an empty slice", positions)
	}
}
