package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/TemirlanKoybaev/tripgoavito.git/internal/tx"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type StatusHistoryRepository struct {
	pool         *pgxpool.Pool
	queryTimeout time.Duration
}

func NewStatusHistoryRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *StatusHistoryRepository {
	return &StatusHistoryRepository{pool: pool, queryTimeout: queryTimeout}
}

func (r *StatusHistoryRepository) Create(ctx context.Context, tripID uuid.UUID, fromStatus *string, toStatus string) error {
	query, args, err := psql.Insert("trip_status_history").
		Columns("trip_id", "from_status", "to_status").
		Values(tripID, fromStatus, toStatus).
		ToSql()
	if err != nil {
		return fmt.Errorf("build query: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	if _, err := tx.GetExecutor(ctx, r.pool).Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("insert status history: %w", err)
	}
	return nil
}
