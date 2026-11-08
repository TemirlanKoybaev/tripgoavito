package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"

	"github.com/TemirlanKoybaev/tripgoavito.git/internal/config"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/container"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	if err := godotenv.Load(); err != nil {
		log.Println("no .env file, reading from environment")
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	c, err := container.New(context.Background(), cfg)
	if err != nil {
		return fmt.Errorf("container: %w", err)
	}
	defer c.Close()

	log.Println("connected to database")

	serverErr := make(chan error, 1)
	go func() {
		log.Printf("starting on %s", cfg.HTTPAddr)
		if err := c.Server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		return fmt.Errorf("server: %w", err)
	case <-quit:
	}

	log.Println("shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := c.Server.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}

	log.Println("server stopped")
	return nil
}
