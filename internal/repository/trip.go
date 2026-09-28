package repository

import (
	"context"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/model"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/tx"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var psql = sq.StatementBuilder.PlaceholderFormat(sq.Dollar)

type TripRepository struct {
	pool *pgxpool.Pool
}

func NewTripRepository(pool *pgxpool.Pool) *TripRepository {
	return &TripRepository{pool: pool}
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

	if t := tx.GetExecutor(ctx, r.pool); t != nil {
		_, err = t.Exec(ctx, query, args...)
	} else {
		_, err = r.pool.Exec(ctx, query, args...)
	}
	return err
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

	var trip model.Trip
	var row pgx.Row
	if t := tx.GetExecutor(ctx, r.pool); t != nil {
		row = t.QueryRow(ctx, query, args...)
	} else {
		row = r.pool.QueryRow(ctx, query, args...)
	}

	err = row.Scan(
		&trip.ID, &trip.UserID, &trip.DriverID,
		&trip.StartLatitude, &trip.StartLongitude,
		&trip.EndLatitude, &trip.EndLongitude,
		&trip.Price, &trip.Status, &trip.StartedAt, &trip.FinishedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}
	return &trip, nil
}

func (r *TripRepository) Finish(ctx context.Context, id uuid.UUID, finishedAt time.Time) (int64, error) {
	query, args, err := psql.Update("trips").
		Set("status", "completed").
		Set("finished_at", finishedAt).
		Set("updated_at", time.Now()).
		Where(sq.And{sq.Eq{"id": id}, sq.Eq{"status": "active"}}).
		ToSql()
	if err != nil {
		return 0, fmt.Errorf("build query: %w", err)
	}

	var rowsAffected int64
	if t := tx.GetExecutor(ctx, r.pool); t != nil {
		result, err := t.Exec(ctx, query, args...)
		if err != nil {
			return 0, err
		}
		rowsAffected = result.RowsAffected()
	} else {
		result, err := r.pool.Exec(ctx, query, args...)
		if err != nil {
			return 0, err
		}
		rowsAffected = result.RowsAffected()
	}
	return rowsAffected, nil
}
