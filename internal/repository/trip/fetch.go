package trip

import (
	"context"
	"errors"
	"fmt"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	domain "github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/tx"
)

var tripColumns = []string{
	"id", "user_id", "driver_id",
	"start_latitude", "start_longitude",
	"end_latitude", "end_longitude",
	"price", "status", "started_at", "finished_at",
}

func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Trip, error) {
	return r.fetch(ctx, id, false)
}

func (r *Repository) GetForUpdate(ctx context.Context, id uuid.UUID) (*domain.Trip, error) {
	return r.fetch(ctx, id, true)
}

func (r *Repository) fetch(ctx context.Context, id uuid.UUID, forUpdate bool) (*domain.Trip, error) {
	builder := psql.Select(tripColumns...).From("trips").Where(sq.Eq{"id": id})
	if forUpdate {
		builder = builder.Suffix("FOR UPDATE")
	}

	query, args, err := builder.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	var t domain.Trip
	err = tx.GetExecutor(ctx, r.pool).QueryRow(ctx, query, args...).Scan(
		&t.ID, &t.UserID, &t.DriverID,
		&t.StartLatitude, &t.StartLongitude,
		&t.EndLatitude, &t.EndLongitude,
		&t.Price, &t.Status, &t.StartedAt, &t.FinishedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrTripNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get trip: %w", err)
	}

	t.StartedAt = t.StartedAt.UTC()
	if t.FinishedAt != nil {
		finishedAt := t.FinishedAt.UTC()
		t.FinishedAt = &finishedAt
	}
	return &t, nil
}
