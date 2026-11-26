package positions_test

import (
	"context"
	"errors"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/usecase/trip/positions"
)

func TestCreateValidatesBeforeTransaction(t *testing.T) {
	now := time.Date(2026, time.November, 27, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		latitude  float64
		longitude float64
		at        time.Time
	}{
		{name: "latitude outside range", latitude: -90.01, at: now},
		{name: "longitude outside range", longitude: 180.01, at: now},
		{name: "NaN coordinate", latitude: math.NaN(), at: now},
		{name: "more than one minute ahead", at: now.Add(time.Minute + time.Nanosecond)},
		{name: "outside RFC 3339 after UTC conversion", at: time.Date(-1, 12, 31, 23, 59, 0, 0, time.UTC)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			u := positions.New(NewMocktripRepository(ctrl), NewMocktxManager(ctrl), NewMockpositionMetrics(ctrl), func() time.Time { return now })
			_, err := u.Create(t.Context(), trip.Position{Latitude: tt.latitude, Longitude: tt.longitude, RecordedAt: tt.at})
			var invalid *trip.ValidationError
			if !errors.As(err, &invalid) {
				t.Fatalf("Create() error = %v, want validation error", err)
			}
		})
	}
}

func TestCreateUsesTransaction(t *testing.T) {
	now := time.Date(2026, time.November, 27, 0, 0, 0, 123, time.UTC)
	input := trip.Position{TripID: uuid.New(), Latitude: -90, Longitude: 180, RecordedAt: now.Add(time.Minute)}
	saved := input
	saved.ID = 42
	saved.RecordedAt = saved.RecordedAt.Truncate(time.Microsecond)
	failure := errors.New("database failure")
	tests := []struct {
		name      string
		status    string
		lookupErr error
		insertErr error
		updateErr error
		commitErr error
		wantErr   error
	}{
		{name: "success at the inclusive time and coordinate boundaries", status: trip.StatusActive},
		{name: "missing trip", lookupErr: trip.ErrTripNotFound, wantErr: trip.ErrTripNotFound},
		{name: "completed trip", status: trip.StatusCompleted, wantErr: trip.ErrTripCompleted},
		{name: "insert failed", status: trip.StatusActive, insertErr: failure, wantErr: failure},
		{name: "update failed", status: trip.StatusActive, updateErr: failure, wantErr: failure},
		{name: "commit failed", status: trip.StatusActive, commitErr: failure, wantErr: failure},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			repo := NewMocktripRepository(ctrl)
			tx := NewMocktxManager(ctrl)
			metrics := NewMockpositionMetrics(ctrl)
			ctx, txCtx := t.Context(), context.WithoutCancel(t.Context())
			tx.EXPECT().Do(ctx, gomock.Any()).DoAndReturn(func(_ context.Context, fn func(context.Context) error) error {
				if err := fn(txCtx); err != nil {
					return err
				}
				return tt.commitErr
			})
			lock := repo.EXPECT().GetForUpdate(txCtx, input.TripID).
				Return(&trip.Trip{ID: input.TripID, Status: tt.status}, tt.lookupErr)
			if tt.status == trip.StatusActive {
				insert := repo.EXPECT().InsertPosition(txCtx, input).After(lock).Return(saved, tt.insertErr)
				if tt.insertErr == nil {
					repo.EXPECT().UpdateLastPositionAt(txCtx, input.TripID, saved.RecordedAt).After(insert).Return(tt.updateErr)
				}
			}

			if tt.wantErr == nil {
				metrics.EXPECT().PositionSaved(ctx)
			}
			got, err := positions.New(repo, tx, metrics, func() time.Time { return now }).Create(ctx, input)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Create() error = %v, want %v", err, tt.wantErr)
			}
			want := saved
			if tt.wantErr != nil {
				want = trip.Position{}
			}
			if got != want {
				t.Errorf("Create() = %+v, want %+v", got, want)
			}
		})
	}
}

func TestListPositions(t *testing.T) {
	id := uuid.New()
	t.Run("completed trip still has a route", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		repo := NewMocktripRepository(ctrl)
		points := []trip.Position{{ID: 42, TripID: id}}
		gomock.InOrder(
			repo.EXPECT().GetByID(t.Context(), id).Return(&trip.Trip{ID: id, Status: trip.StatusCompleted}, nil),
			repo.EXPECT().ListPositions(t.Context(), id).Return(points, nil),
		)
		got, err := positions.New(repo, NewMocktxManager(ctrl), NewMockpositionMetrics(ctrl), time.Now).List(t.Context(), id)
		if err != nil || !slices.Equal(got, points) {
			t.Fatalf("List() = %+v, %v, want %+v", got, err, points)
		}
	})
	t.Run("missing trip", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		repo := NewMocktripRepository(ctrl)
		repo.EXPECT().GetByID(t.Context(), id).Return(nil, trip.ErrTripNotFound)
		got, err := positions.New(repo, NewMocktxManager(ctrl), NewMockpositionMetrics(ctrl), time.Now).List(t.Context(), id)
		if got != nil || !errors.Is(err, trip.ErrTripNotFound) {
			t.Fatalf("List() = %+v, %v, want trip not found", got, err)
		}
	})
}
