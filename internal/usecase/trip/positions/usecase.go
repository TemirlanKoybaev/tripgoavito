package positions

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
)

type UseCase struct {
	trips   tripRepository
	tx      txManager
	metrics positionMetrics
	now     func() time.Time
}

func New(trips tripRepository, tx txManager, metrics positionMetrics, now func() time.Time) *UseCase {
	return &UseCase{trips: trips, tx: tx, metrics: metrics, now: now}
}

func (u *UseCase) Create(ctx context.Context, position trip.Position) (trip.Position, error) {
	if err := validate(position, u.now()); err != nil {
		return trip.Position{}, err
	}

	var saved trip.Position
	err := u.tx.Do(ctx, func(ctx context.Context) error {
		// Finish берёт ту же блокировку строки до смены статуса.
		t, err := u.trips.GetForUpdate(ctx, position.TripID)
		if err != nil {
			return err
		}
		if t.Status != trip.StatusActive {
			return trip.ErrTripCompleted
		}
		saved, err = u.trips.InsertPosition(ctx, position)
		if err != nil {
			return err
		}
		return u.trips.UpdateLastPositionAt(ctx, saved.TripID, saved.RecordedAt)
	})
	if err != nil {
		return trip.Position{}, err
	}
	u.metrics.PositionSaved(ctx)
	return saved, nil
}

func (u *UseCase) List(ctx context.Context, tripID uuid.UUID) ([]trip.Position, error) {
	if _, err := u.trips.GetByID(ctx, tripID); err != nil {
		return nil, err
	}
	return u.trips.ListPositions(ctx, tripID)
}

func validate(position trip.Position, now time.Time) error {
	if !(position.Latitude >= -90 && position.Latitude <= 90) {
		return &trip.ValidationError{Message: "coordinates.latitude must be between -90 and 90"}
	}
	if !(position.Longitude >= -180 && position.Longitude <= 180) {
		return &trip.ValidationError{Message: "coordinates.longitude must be between -180 and 180"}
	}
	if position.RecordedAt.After(now.Add(time.Minute)) {
		return &trip.ValidationError{Message: "recorded_at must be at most one minute in the future"}
	}
	if year := position.RecordedAt.UTC().Year(); year < 0 || year > 9999 {
		return &trip.ValidationError{Message: "recorded_at must be representable as RFC 3339 in UTC"}
	}
	return nil
}
