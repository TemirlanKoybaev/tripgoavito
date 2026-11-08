package container

import (
	"context"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TemirlanKoybaev/tripgoavito.git/internal/config"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/http/health"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/http/router"
	createhttp "github.com/TemirlanKoybaev/tripgoavito.git/internal/http/trip/create"
	finishhttp "github.com/TemirlanKoybaev/tripgoavito.git/internal/http/trip/finish"
	gethttp "github.com/TemirlanKoybaev/tripgoavito.git/internal/http/trip/get"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/infra/postgres"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/infra/postgres/transaction"
	historyrepo "github.com/TemirlanKoybaev/tripgoavito.git/internal/repository/history"
	triprepo "github.com/TemirlanKoybaev/tripgoavito.git/internal/repository/trip"
	createuc "github.com/TemirlanKoybaev/tripgoavito.git/internal/usecase/trip/create"
	finishuc "github.com/TemirlanKoybaev/tripgoavito.git/internal/usecase/trip/finish"
	getuc "github.com/TemirlanKoybaev/tripgoavito.git/internal/usecase/trip/get"
)

type Container struct {
	Server *http.Server
	pool   *pgxpool.Pool
}

func New(ctx context.Context, cfg *config.Config) (*Container, error) {
	pool, err := postgres.Connect(
		ctx,
		cfg.DatabaseURL,
		cfg.DatabaseMaxConns,
		cfg.DatabaseMinConns,
		cfg.DatabaseMaxConnLifetime,
		cfg.DatabaseConnectTimeout,
	)
	if err != nil {
		return nil, fmt.Errorf("connect db: %w", err)
	}

	txManager := transaction.New(pool)
	trips := triprepo.New(pool, cfg.DatabaseQueryTimeout)
	history := historyrepo.New(pool, cfg.DatabaseQueryTimeout)

	createHandler := createhttp.New(createuc.New(trips, history, txManager))
	getHandler := gethttp.New(getuc.New(trips))
	finishHandler := finishhttp.New(finishuc.New(trips, history, txManager))
	healthHandler := health.New(pool, cfg.DatabaseQueryTimeout)

	handler := router.New(createHandler, getHandler, finishHandler, healthHandler).Handler()

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadTimeout:       cfg.HTTPReadTimeout,
		ReadHeaderTimeout: cfg.HTTPReadHeaderTimeout,
		WriteTimeout:      cfg.HTTPWriteTimeout,
		IdleTimeout:       cfg.HTTPIdleTimeout,
	}

	return &Container{Server: server, pool: pool}, nil
}

func (c *Container) Close() {
	c.pool.Close()
}
