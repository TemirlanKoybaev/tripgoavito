package trip

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	domain "github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/infra/postgres/transaction"
)

const effectivePositionAt = "COALESCE(last_position_at, started_at)"

// ListStaleTrips возвращает страницу поездок с эффективным временем раньше cutoff.
// Все страницы одного прохода используют один cutoff. Для первой страницы after равен nil.
func (r *Repository) ListStaleTrips(ctx context.Context, cutoff time.Time, after *domain.StaleTripCursor, limit int) ([]domain.StaleTrip, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("stale trips batch size must be positive")
	}

	// Литеральный статус позволяет использовать частичный индекс и в общем плане PostgreSQL.
	builder := psql.Select("id", "driver_id", effectivePositionAt).
		From("trips").
		Where("status = 'active'").
		Where(effectivePositionAt+" < ?", cutoff).
		OrderBy(effectivePositionAt+" ASC", "id ASC").
		Suffix("LIMIT ?", limit)
	if after != nil {
		builder = builder.Where("("+effectivePositionAt+", id) > (?, ?)", after.EffectiveAt, after.TripID)
	}

	query, args, err := builder.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build stale trips query: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	rows, err := transaction.GetExecutor(ctx, r.pool).Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list stale trips: %w", err)
	}
	trips, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.StaleTrip, error) {
		var tr domain.StaleTrip
		err := row.Scan(&tr.TripID, &tr.DriverID, &tr.EffectiveAt)
		tr.EffectiveAt = tr.EffectiveAt.UTC()
		return tr, err
	})
	if err != nil {
		return nil, fmt.Errorf("read stale trips: %w", err)
	}
	return trips, nil
}
