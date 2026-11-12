package trip

import (
	"context"
	"fmt"

	domain "github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/infra/postgres/transaction"
)

// CountActive возвращает число поездок в статусе active. Индекс
// trips_status_started_at_idx покрывает этот фильтр.
func (r *Repository) CountActive(ctx context.Context) (int64, error) {
	query, args, err := psql.Select("count(*)").
		From("trips").
		Where("status = ?", domain.StatusActive).
		ToSql()
	if err != nil {
		return 0, fmt.Errorf("build query: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	var n int64
	if err := transaction.GetExecutor(ctx, r.pool).QueryRow(ctx, query, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("count active trips: %w", err)
	}
	return n, nil
}
