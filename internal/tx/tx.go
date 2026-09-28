package tx

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type contextKey struct{}

type TxManager struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *TxManager {
	return &TxManager{pool: pool}
}

func (m *TxManager) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	if txFromCtx(ctx) != nil {
		return fn(ctx)
	}

	transaction, err := m.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.ReadCommitted,
	})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			transaction.Rollback(ctx)
			panic(p)
		}
	}()

	ctx = context.WithValue(ctx, contextKey{}, transaction)

	if err := fn(ctx); err != nil {
		transaction.Rollback(ctx)
		return err
	}

	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}

func txFromCtx(ctx context.Context) pgx.Tx {
	tx, _ := ctx.Value(contextKey{}).(pgx.Tx)
	return tx
}

func GetExecutor(ctx context.Context, pool *pgxpool.Pool) pgx.Tx {
	return txFromCtx(ctx)
}
