package finish

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
)

type UseCase struct {
	trips   tripRepository
	history historyRepository
	tx      txManager
}

func New(trips tripRepository, history historyRepository, tx txManager) *UseCase {
	return &UseCase{trips: trips, history: history, tx: tx}
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
	return result, nil
}
