package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/multitracer"
	"github.com/jackc/pgx/v5/pgxpool"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
)

func Connect(ctx context.Context, databaseURL string, maxConns, minConns int32, maxConnLifetime, connectTimeout time.Duration, recorder OperationRecorder) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	cfg.MaxConns = maxConns
	cfg.MinConns = minConns
	cfg.MaxConnLifetime = maxConnLifetime
	cfg.ConnConfig.ConnectTimeout = connectTimeout
	// Метрики otelpgx отключены (noop): db.client.operation.duration пишет
	// metricsTracer, потому что у otelpgx нет db.collection.name и error.type.
	tracer := otelpgx.NewTracer(otelpgx.WithMeterProvider(metricnoop.NewMeterProvider()))
	// metricsTracer первым: его TraceQueryEnd должен отработать до span.End у otelpgx.
	if recorder != nil {
		cfg.ConnConfig.Tracer = multitracer.New(metricsTracer{recorder: recorder}, tracer)
	} else {
		cfg.ConnConfig.Tracer = tracer
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping: %w", err)
	}

	return pool, nil
}
