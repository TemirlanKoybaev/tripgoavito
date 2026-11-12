package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/model"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/tx"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	pgUniqueViolation = "23505"
	driverActiveIndex = "trips_driver_active_idx"
)

var psql = sq.StatementBuilder.PlaceholderFormat(sq.Dollar)

type TripRepository struct {
	pool         *pgxpool.Pool
	queryTimeout time.Duration
}

func NewTripRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *TripRepository {
	return &TripRepository{pool: pool, queryTimeout: queryTimeout}
}

func (r *TripRepository) Create(ctx context.Context, trip *model.Trip) error {
	query, args, err := psql.Insert("trips").
		Columns("id", "user_id", "driver_id",
			"start_latitude", "start_longitude",
			"end_latitude", "end_longitude",
			"price", "status", "started_at").
		Values(trip.ID, trip.UserID, trip.DriverID,
			trip.StartLatitude, trip.StartLongitude,
			trip.EndLatitude, trip.EndLongitude,
			trip.Price, trip.Status, trip.StartedAt).
		ToSql()
	if err != nil {
		return fmt.Errorf("build query: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	_, err = tx.GetExecutor(ctx, r.pool).Exec(ctx, query, args...)

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == driverActiveIndex {
		return model.ErrDriverBusy
	}
	if err != nil {
		return fmt.Errorf("insert trip: %w", err)
	}
	return nil
}

func (r *TripRepository) GetByID(ctx context.Context, id uuid.UUID) (*model.Trip, error) {
	query, args, err := psql.Select(
		"id", "user_id", "driver_id",
		"start_latitude", "start_longitude",
		"end_latitude", "end_longitude",
		"price", "status", "started_at", "finished_at",
	).From("trips").Where(sq.Eq{"id": id}).ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	var trip model.Trip
	err = tx.GetExecutor(ctx, r.pool).QueryRow(ctx, query, args...).Scan(
		&trip.ID, &trip.UserID, &trip.DriverID,
		&trip.StartLatitude, &trip.StartLongitude,
		&trip.EndLatitude, &trip.EndLongitude,
		&trip.Price, &trip.Status, &trip.StartedAt, &trip.FinishedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, model.ErrTripNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get trip: %w", err)
	}

	trip.StartedAt = trip.StartedAt.UTC()
	if trip.FinishedAt != nil {
		finishedAt := trip.FinishedAt.UTC()
		trip.FinishedAt = &finishedAt
	}
	return &trip, nil
}

func (r *TripRepository) Finish(ctx context.Context, id uuid.UUID, finishedAt time.Time) (int64, error) {
	query, args, err := psql.Update("trips").
		Set("status", model.StatusCompleted).
		Set("finished_at", finishedAt).
		Set("updated_at", finishedAt).
		Where(sq.And{sq.Eq{"id": id}, sq.Eq{"status": model.StatusActive}}).
		ToSql()
	if err != nil {
		return 0, fmt.Errorf("build query: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	result, err := tx.GetExecutor(ctx, r.pool).Exec(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("finish trip: %w", err)
	}
	return result.RowsAffected(), nil
}
