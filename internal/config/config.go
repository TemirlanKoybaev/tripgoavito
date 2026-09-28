package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	HTTPAddr        string
	LogLevel        string
	ShutdownTimeout time.Duration

	DatabaseURL             string
	DatabaseMaxConns        int32
	DatabaseMinConns        int32
	DatabaseMaxConnLifetime time.Duration
	DatabaseConnectTimeout  time.Duration
	DatabaseQueryTimeout    time.Duration
}

func Load() (*Config, error) {
	cfg := &Config{
		HTTPAddr:    getEnv("HTTP_ADDR", ":8080"),
		LogLevel:    getEnv("LOG_LEVEL", "info"),
		DatabaseURL: getEnv("DATABASE_URL", ""),
	}

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	var err error

	cfg.ShutdownTimeout, err = time.ParseDuration(getEnv("SHUTDOWN_TIMEOUT", "10s"))
	if err != nil {
		return nil, fmt.Errorf("SHUTDOWN_TIMEOUT: %w", err)
	}

	maxConns, err := strconv.Atoi(getEnv("DATABASE_MAX_CONNS", "10"))
	if err != nil {
		return nil, fmt.Errorf("DATABASE_MAX_CONNS: %w", err)
	}
	cfg.DatabaseMaxConns = int32(maxConns)

	minConns, err := strconv.Atoi(getEnv("DATABASE_MIN_CONNS", "2"))
	if err != nil {
		return nil, fmt.Errorf("DATABASE_MIN_CONNS: %w", err)
	}
	cfg.DatabaseMinConns = int32(minConns)

	cfg.DatabaseMaxConnLifetime, err = time.ParseDuration(getEnv("DATABASE_MAX_CONN_LIFETIME", "30m"))
	if err != nil {
		return nil, fmt.Errorf("DATABASE_MAX_CONN_LIFETIME: %w", err)
	}

	cfg.DatabaseConnectTimeout, err = time.ParseDuration(getEnv("DATABASE_CONNECT_TIMEOUT", "5s"))
	if err != nil {
		return nil, fmt.Errorf("DATABASE_CONNECT_TIMEOUT: %w", err)
	}

	cfg.DatabaseQueryTimeout, err = time.ParseDuration(getEnv("DATABASE_QUERY_TIMEOUT", "3s"))
	if err != nil {
		return nil, fmt.Errorf("DATABASE_QUERY_TIMEOUT: %w", err)
	}

	return cfg, nil
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
