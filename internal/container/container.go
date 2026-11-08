package container

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TemirlanKoybaev/tripgoavito.git/internal/config"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/http/health"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/http/middleware"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/http/router"
	createhttp "github.com/TemirlanKoybaev/tripgoavito.git/internal/http/trip/create"
	finishhttp "github.com/TemirlanKoybaev/tripgoavito.git/internal/http/trip/finish"
	gethttp "github.com/TemirlanKoybaev/tripgoavito.git/internal/http/trip/get"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/infra/logger"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/infra/postgres"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/infra/postgres/transaction"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/infra/telemetry"
	historyrepo "github.com/TemirlanKoybaev/tripgoavito.git/internal/repository/history"
	triprepo "github.com/TemirlanKoybaev/tripgoavito.git/internal/repository/trip"
	createuc "github.com/TemirlanKoybaev/tripgoavito.git/internal/usecase/trip/create"
	finishuc "github.com/TemirlanKoybaev/tripgoavito.git/internal/usecase/trip/finish"
	getuc "github.com/TemirlanKoybaev/tripgoavito.git/internal/usecase/trip/get"
)

const telemetryShutdownTimeout = 5 * time.Second

type Container struct {
	Server            *http.Server
	pool              *pgxpool.Pool
	shutdownTelemetry func(context.Context) error
}

func New(ctx context.Context, cfg *config.Config) (*Container, error) {
	lg := logger.New(cfg.LogLevel, cfg.ServiceName)
	metrics, shutdownTelemetry, err := telemetry.Setup(ctx, cfg.ServiceVersion)
	if err != nil {
		return nil, fmt.Errorf("setup telemetry: %w", err)
	}

	pool, err := postgres.Connect(
		ctx,
		cfg.DatabaseURL,
		cfg.DatabaseMaxConns,
		cfg.DatabaseMinConns,
		cfg.DatabaseMaxConnLifetime,
		cfg.DatabaseConnectTimeout,
		metrics,
	)
	if err != nil {
		flushTelemetry(shutdownTelemetry)
		return nil, fmt.Errorf("connect db: %w", err)
	}

	txManager := transaction.New(pool)
	trips := triprepo.New(pool, cfg.DatabaseQueryTimeout)
	history := historyrepo.New(pool, cfg.DatabaseQueryTimeout)

	createHandler := createhttp.New(createuc.New(trips, history, txManager, metrics, lg))
	getHandler := gethttp.New(getuc.New(trips))
	finishHandler := finishhttp.New(finishuc.New(trips, history, txManager, metrics, lg))
	healthHandler := health.New(pool, cfg.DatabaseQueryTimeout)

	handler := router.New(createHandler, getHandler, finishHandler, healthHandler).Handler()
	handler = middleware.Metrics(metrics)(handler)
	handler = middleware.Logging(lg)(handler)
	handler = middleware.Tracing(handler)
	handler = middleware.Recover(lg)(handler)

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadTimeout:       cfg.HTTPReadTimeout,
		ReadHeaderTimeout: cfg.HTTPReadHeaderTimeout,
		WriteTimeout:      cfg.HTTPWriteTimeout,
		IdleTimeout:       cfg.HTTPIdleTimeout,
	}

	return &Container{Server: server, pool: pool, shutdownTelemetry: shutdownTelemetry}, nil
}

func (c *Container) Close() {
	c.pool.Close()
	flushTelemetry(c.shutdownTelemetry)
}

func flushTelemetry(shutdown func(context.Context) error) {
	ctx, cancel := context.WithTimeout(context.Background(), telemetryShutdownTimeout)
	defer cancel()

	if err := shutdown(ctx); err != nil {
		log.Printf("shutdown telemetry: %v", err)
	}
}
