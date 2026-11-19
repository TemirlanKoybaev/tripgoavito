package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"

	"github.com/TemirlanKoybaev/tripgoavito.git/internal/config"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/container"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/infra/logger"
)

const defaultServiceName = "trip-service"

func main() {
	os.Exit(run())
}

func run() int {
	// Логгера ещё нет (его уровень задаёт конфиг), поэтому результат godotenv
	// запоминаем и пишем в лог, как только он появится.
	envErr := godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		logger.New("info", defaultServiceName).Error("load config", slog.Any("error", err))
		return 1
	}

	lg := logger.New(cfg.LogLevel, cfg.ServiceName)
	if envErr != nil {
		lg.Debug("no .env file, reading from environment")
	}

	if err := serve(cfg, lg); err != nil {
		lg.Error("service failed", slog.Any("error", err))
		return 1
	}
	return 0
}

func serve(cfg *config.Config, lg *slog.Logger) error {
	lg.Info("service starting", slog.String("version", cfg.ServiceVersion))

	c, err := container.New(context.Background(), cfg, lg)
	if err != nil {
		return fmt.Errorf("container: %w", err)
	}
	defer c.Close()

	serverErr := make(chan error, 1)
	go func() {
		lg.Info("http server starting", slog.String("addr", cfg.HTTPAddr))
		if err := c.Server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		return fmt.Errorf("server: %w", err)
	case sig := <-quit:
		lg.Info("service stopping", slog.String("signal", sig.String()))
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := c.Server.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}

	lg.Info("http server stopped")
	lg.Info("service stopped")
	return nil
}
