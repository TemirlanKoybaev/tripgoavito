package repository

import (
	"context"
	"fmt"

	"github.com/TemirlanKoybaev/tripgoavito.git/internal/tx"
	"github.com/google/uuid"
)

type TripStatusHistoryRepository struct {
	pool interface {
		Exec(ctx context.Context, sql string, args ...any) (interface{ RowsAffected() int64 }, error)
	}
}

type StatusHistoryRepository struct {
	tripRepo *TripRepository
}

func NewStatusHistoryRepository(tripRepo *TripRepository) *StatusHistoryRepository {
	return &StatusHistoryRepository{tripRepo: tripRepo}
}

func (r *StatusHistoryRepository) Create(ctx context.Context, tripID uuid.UUID, fromStatus *string, toStatus string) error {
	query, args, err := psql.Insert("trip_status_history").
		Columns("trip_id", "from_status", "to_status").
		Values(tripID, fromStatus, toStatus).
		ToSql()
	if err != nil {
		return fmt.Errorf("build query: %w", err)
	}

	if t := tx.GetExecutor(ctx, r.tripRepo.pool); t != nil {
		_, err = t.Exec(ctx, query, args...)
	} else {
		_, err = r.tripRepo.pool.Exec(ctx, query, args...)
	}
	return err
}
