package trip

import (
	"context"
	"fmt"
	"time"

	domain "github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/infra/postgres/transaction"
)

func (r *Repository) Create(ctx context.Context, t *domain.Trip) error {
	query, args, err := psql.Insert("trips").
		Columns("id", "user_id", "driver_id",
			"start_latitude", "start_longitude",
			"end_latitude", "end_longitude",
			"price", "status", "started_at").
		Values(t.ID, t.UserID, t.DriverID,
			t.StartLatitude, t.StartLongitude,
			t.EndLatitude, t.EndLongitude,
			t.Price, t.Status, t.StartedAt).
		Suffix("ON CONFLICT (driver_id) WHERE status = 'active' DO NOTHING").
		ToSql()
	if err != nil {
		return fmt.Errorf("build query: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	result, err := transaction.GetExecutor(ctx, r.pool).Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("insert trip: %w", err)
	}
	if result.RowsAffected() == 0 {
		return domain.ErrDriverBusy
	}
	return nil
}

func (r *Repository) Update(ctx context.Context, t *domain.Trip) error {
	updatedAt := time.Now().UTC().Truncate(time.Microsecond)
	if t.FinishedAt != nil {
		updatedAt = *t.FinishedAt
	}

	query, args, err := psql.Update("trips").
		Set("status", t.Status).
		Set("finished_at", t.FinishedAt).
		Set("updated_at", updatedAt).
		Where("id = ?", t.ID).
		ToSql()
	if err != nil {
		return fmt.Errorf("build query: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	result, err := transaction.GetExecutor(ctx, r.pool).Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update trip: %w", err)
	}
	if result.RowsAffected() == 0 {
		return domain.ErrTripNotFound
	}
	return nil
}
