package create

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
)

type Input struct {
	UserID         uuid.UUID
	DriverID       uuid.UUID
	StartLatitude  float64
	StartLongitude float64
	EndLatitude    float64
	EndLongitude   float64
	Price          int64
}

type UseCase struct {
	trips   tripRepository
	history historyRepository
	tx      txManager
	metrics tripMetrics
	log     *slog.Logger
}

func New(trips tripRepository, history historyRepository, tx txManager, metrics tripMetrics, log *slog.Logger) *UseCase {
	return &UseCase{trips: trips, history: history, tx: tx, metrics: metrics, log: log}
}

func (u *UseCase) Create(ctx context.Context, in Input) (*trip.Trip, error) {
	t := &trip.Trip{
		ID:             uuid.New(),
		UserID:         in.UserID,
		DriverID:       in.DriverID,
		StartLatitude:  in.StartLatitude,
		StartLongitude: in.StartLongitude,
		EndLatitude:    in.EndLatitude,
		EndLongitude:   in.EndLongitude,
		Price:          in.Price,
		Status:         trip.StatusActive,
		StartedAt:      time.Now().UTC().Truncate(time.Microsecond),
	}

	err := u.tx.Do(ctx, func(ctx context.Context) error {
		if err := u.trips.Create(ctx, t); err != nil {
			return err
		}
		return u.history.Create(ctx, t.ID, nil, t.Status)
	})
	if err != nil {
		return nil, err
	}

	// Счётчик и лог только после commit: откатанная поездка не считается созданной.
	u.metrics.TripCreated(ctx)
	u.log.InfoContext(ctx, "trip created", slog.String("trip_id", t.ID.String()))
	return t, nil
}
