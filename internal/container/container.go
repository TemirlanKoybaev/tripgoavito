package container

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"

	"github.com/TemirlanKoybaev/tripgoavito.git/internal/config"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/gateway/push"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/http/health"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/http/middleware"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/http/router"
	createhttp "github.com/TemirlanKoybaev/tripgoavito.git/internal/http/trip/create"
	finishhttp "github.com/TemirlanKoybaev/tripgoavito.git/internal/http/trip/finish"
	gethttp "github.com/TemirlanKoybaev/tripgoavito.git/internal/http/trip/get"
	positionshttp "github.com/TemirlanKoybaev/tripgoavito.git/internal/http/trip/positions"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/infra/postgres"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/infra/postgres/transaction"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/infra/telemetry"
	historyrepo "github.com/TemirlanKoybaev/tripgoavito.git/internal/repository/history"
	triprepo "github.com/TemirlanKoybaev/tripgoavito.git/internal/repository/trip"
	createuc "github.com/TemirlanKoybaev/tripgoavito.git/internal/usecase/trip/create"
	finishuc "github.com/TemirlanKoybaev/tripgoavito.git/internal/usecase/trip/finish"
	getuc "github.com/TemirlanKoybaev/tripgoavito.git/internal/usecase/trip/get"
	positionsuc "github.com/TemirlanKoybaev/tripgoavito.git/internal/usecase/trip/positions"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/worker/positions"
)

const telemetryErrorLogEvery = 30 * time.Second

type Container struct {
	Server            *http.Server
	pool              *pgxpool.Pool
	push              *push.Client
	scanner           *positions.Scanner
	workers           *positions.Pool
	shutdownTelemetry func(context.Context) error
	log               *slog.Logger
	releaseOnce       sync.Once
	released          chan struct{}
}

func New(ctx context.Context, cfg *config.Config, lg *slog.Logger) (_ *Container, buildErr error) {
	// Ошибки самого OTel SDK (коллектор недоступен и т. п.) идут в наш лог, а не в стандартный,
	// и не чаще раза в telemetryErrorLogEvery: иначе недоступный коллектор забьёт лог.
	otel.SetErrorHandler(telemetry.NewErrorHandler(lg, telemetryErrorLogEvery))

	metrics, shutdownTelemetry, err := telemetry.Setup(ctx, cfg.ServiceVersion)
	if err != nil {
		return nil, fmt.Errorf("setup telemetry: %w", err)
	}
	c := &Container{shutdownTelemetry: shutdownTelemetry, log: lg, released: make(chan struct{})}
	defer func() {
		if buildErr != nil {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.ShutdownTimeout)
			defer cancel()
			if c.workers != nil {
				buildErr = errors.Join(buildErr, c.workers.Shutdown(cleanupCtx))
			}
			buildErr = errors.Join(buildErr, c.releaseResources(cleanupCtx))
		}
	}()

	c.push, err = push.New(cfg.PushHTTPURL, cfg.PushRequestTimeout)
	if err != nil {
		return nil, fmt.Errorf("setup push: %w", err)
	}

	c.pool, err = postgres.Connect(
		ctx,
		cfg.DatabaseURL,
		cfg.DatabaseMaxConns,
		cfg.DatabaseMinConns,
		cfg.DatabaseMaxConnLifetime,
		cfg.DatabaseConnectTimeout,
		metrics,
	)
	if err != nil {
		return nil, fmt.Errorf("connect db: %w", err)
	}

	txManager := transaction.New(c.pool, lg)
	trips := triprepo.New(c.pool, cfg.DatabaseQueryTimeout)
	history := historyrepo.New(c.pool, cfg.DatabaseQueryTimeout)

	if err := metrics.RegisterActiveTrips(trips); err != nil {
		return nil, fmt.Errorf("register active trips gauge: %w", err)
	}
	c.workers, err = positions.NewPool(ctx, c.push, metrics, cfg.PositionWorkers, cfg.PositionQueueSize, cfg.PushRequestTimeout, lg)
	if err != nil {
		return nil, fmt.Errorf("start position workers: %w", err)
	}
	if err := metrics.RegisterPositionPool(c.workers); err != nil {
		return nil, fmt.Errorf("register position pool gauges: %w", err)
	}
	c.scanner = positions.NewScanner(ctx, trips, c.workers, metrics, positions.NewTicker(cfg.PositionCheckInterval),
		cfg.PositionStaleAfter, cfg.PositionScanBatchSize, lg)

	createHandler := createhttp.New(createuc.New(trips, history, txManager, metrics, lg))
	getHandler := gethttp.New(getuc.New(trips))
	finishHandler := finishhttp.New(finishuc.New(trips, history, txManager, metrics, lg))
	positionsHandler := positionshttp.New(positionsuc.New(trips, txManager, metrics, c.workers, time.Now))
	healthHandler := health.New(c.pool, cfg.DatabaseQueryTimeout)

	handler := router.New(createHandler, getHandler, finishHandler, positionsHandler, healthHandler, lg).Handler()

	c.Server = &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           withHTTPMiddleware(handler, metrics, lg),
		ReadTimeout:       cfg.HTTPReadTimeout,
		ReadHeaderTimeout: cfg.HTTPReadHeaderTimeout,
		WriteTimeout:      cfg.HTTPWriteTimeout,
		IdleTimeout:       cfg.HTTPIdleTimeout,
	}

	return c, nil
}

func withHTTPMiddleware(handler http.Handler, metrics *telemetry.Metrics, lg *slog.Logger) http.Handler {
	handler = middleware.Recover(lg)(handler)
	handler = middleware.Metrics(metrics)(handler)
	handler = middleware.Logging(lg)(handler)
	return middleware.Tracing(handler)
}

// Shutdown проводит все этапы остановки в рамках дедлайна ctx.
func (c *Container) Shutdown(ctx context.Context) error {
	var errs []error
	if err := c.Server.Shutdown(ctx); err != nil {
		errs = append(errs, fmt.Errorf("shutdown HTTP: %w", err), c.Server.Close())
	}
	c.log.InfoContext(ctx, "http server stopped")
	if err := c.scanner.Shutdown(ctx); err != nil {
		errs = append(errs, fmt.Errorf("shutdown position scanner: %w", err))
	}
	if err := c.workers.Shutdown(ctx); err != nil {
		errs = append(errs, fmt.Errorf("shutdown position workers: %w", err))
	}
	return errors.Join(append(errs, c.releaseResources(ctx))...)
}

func (c *Container) releaseResources(ctx context.Context) error {
	c.releaseOnce.Do(func() {
		if c.push != nil {
			c.push.CloseIdleConnections()
		}
		// Финальный сбор метрик читает tripgo.trips.active из PostgreSQL.
		// Закрытие пула может ждать возврата соединений, поэтому ожидание ограничивает ctx.
		go func() {
			defer close(c.released)
			if err := c.shutdownTelemetry(ctx); err != nil {
				c.log.WarnContext(ctx, "shutdown telemetry", slog.Any("error", err))
			}
			if c.pool != nil {
				c.pool.Close()
			}
		}()
	})
	select {
	case <-c.released:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
