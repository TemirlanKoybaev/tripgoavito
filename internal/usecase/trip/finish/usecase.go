package finish

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
)

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

func (u *UseCase) Finish(ctx context.Context, id uuid.UUID) (*trip.Trip, error) {
	var result *trip.Trip

	err := u.tx.Do(ctx, func(ctx context.Context) error {
		t, err := u.trips.GetForUpdate(ctx, id)
		if err != nil {
			return err
		}

		from := t.Status
		if err := t.Finish(time.Now().UTC().Truncate(time.Microsecond)); err != nil {
			return err
		}

		if err := u.trips.Update(ctx, t); err != nil {
			return err
		}

		if err := u.history.Create(ctx, t.ID, &from, t.Status); err != nil {
			return err
		}

		result = t
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Счётчик и лог только после commit: откатанное завершение не считается.
	u.metrics.TripCompleted(ctx)
	u.log.InfoContext(ctx, "trip completed", slog.String("trip_id", result.ID.String()))
	return result, nil
}
