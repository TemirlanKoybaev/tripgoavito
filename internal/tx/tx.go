package tx

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type contextKey struct{}

type TxManager interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

type DBTX interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type pgManager struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) TxManager {
	return &pgManager{pool: pool}
}

func (m *pgManager) Do(ctx context.Context, fn func(ctx context.Context) error) error {
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
			rollback(ctx, transaction)
			panic(p)
		}
	}()

	ctx = context.WithValue(ctx, contextKey{}, transaction)

	if err := fn(ctx); err != nil {
		rollback(ctx, transaction)
		return err
	}

	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}

func rollback(ctx context.Context, t pgx.Tx) {
	rbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	if err := t.Rollback(rbCtx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		log.Printf("rollback: %v", err)
	}
}

func txFromCtx(ctx context.Context) pgx.Tx {
	tx, _ := ctx.Value(contextKey{}).(pgx.Tx)
	return tx
}

func GetExecutor(ctx context.Context, pool DBTX) DBTX {
	if t := txFromCtx(ctx); t != nil {
		return t
	}
	return pool
}
