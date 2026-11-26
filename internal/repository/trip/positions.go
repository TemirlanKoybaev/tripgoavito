package trip

import (
	"context"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	domain "github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/infra/postgres/transaction"
)

func (r *Repository) InsertPosition(ctx context.Context, position domain.Position) (domain.Position, error) {
	query, args, err := psql.Insert("trip_positions").
		Columns("trip_id", "latitude", "longitude", "recorded_at").
		Values(position.TripID, position.Latitude, position.Longitude, position.RecordedAt).
		Suffix("RETURNING id, recorded_at").
		ToSql()
	if err != nil {
		return domain.Position{}, fmt.Errorf("build query: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	if err := transaction.GetExecutor(ctx, r.pool).QueryRow(ctx, query, args...).Scan(
		&position.ID, &position.RecordedAt,
	); err != nil {
		return domain.Position{}, fmt.Errorf("insert trip position: %w", err)
	}
	position.RecordedAt = position.RecordedAt.UTC()
	return position, nil
}

func (r *Repository) UpdateLastPositionAt(ctx context.Context, tripID uuid.UUID, recordedAt time.Time) error {
	// PostgreSQL пропускает NULL в GREATEST, поэтому первая точка заполняет колонку.
	query, args, err := psql.Update("trips").
		Set("last_position_at", sq.Expr("GREATEST(last_position_at, ?)", recordedAt)).
		Where(sq.Eq{"id": tripID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build query: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	result, err := transaction.GetExecutor(ctx, r.pool).Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update last position time: %w", err)
	}
	if result.RowsAffected() == 0 {
		return domain.ErrTripNotFound
	}
	return nil
}

func (r *Repository) ListPositions(ctx context.Context, tripID uuid.UUID) ([]domain.Position, error) {
	query, args, err := psql.Select("id", "trip_id", "latitude", "longitude", "recorded_at").
		From("trip_positions").
		Where(sq.Eq{"trip_id": tripID}).
		OrderBy("recorded_at ASC", "id ASC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	rows, err := transaction.GetExecutor(ctx, r.pool).Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list trip positions: %w", err)
	}
	positions, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Position, error) {
		var position domain.Position
		err := row.Scan(&position.ID, &position.TripID, &position.Latitude, &position.Longitude, &position.RecordedAt)
		position.RecordedAt = position.RecordedAt.UTC()
		return position, err
	})
	if err != nil {
		return nil, fmt.Errorf("read trip positions: %w", err)
	}
	return positions, nil
}
