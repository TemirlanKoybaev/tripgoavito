package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	HTTPAddr              string
	LogLevel              string
	ShutdownTimeout       time.Duration
	HTTPReadTimeout       time.Duration
	HTTPReadHeaderTimeout time.Duration
	HTTPWriteTimeout      time.Duration
	HTTPIdleTimeout       time.Duration

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

	durations := []struct {
		key string
		def string
		dst *time.Duration
	}{
		{"SHUTDOWN_TIMEOUT", "10s", &cfg.ShutdownTimeout},
		{"HTTP_READ_TIMEOUT", "5s", &cfg.HTTPReadTimeout},
		{"HTTP_READ_HEADER_TIMEOUT", "2s", &cfg.HTTPReadHeaderTimeout},
		{"HTTP_WRITE_TIMEOUT", "10s", &cfg.HTTPWriteTimeout},
		{"HTTP_IDLE_TIMEOUT", "60s", &cfg.HTTPIdleTimeout},
		{"DATABASE_MAX_CONN_LIFETIME", "30m", &cfg.DatabaseMaxConnLifetime},
		{"DATABASE_CONNECT_TIMEOUT", "5s", &cfg.DatabaseConnectTimeout},
		{"DATABASE_QUERY_TIMEOUT", "3s", &cfg.DatabaseQueryTimeout},
	}
	for _, d := range durations {
		v, err := parseDuration(d.key, d.def)
		if err != nil {
			return nil, err
		}
		*d.dst = v
	}

	var err error
	cfg.DatabaseMaxConns, err = parseInt32(getEnv("DATABASE_MAX_CONNS", "10"), "DATABASE_MAX_CONNS")
	if err != nil {
		return nil, err
	}
	cfg.DatabaseMinConns, err = parseInt32(getEnv("DATABASE_MIN_CONNS", "2"), "DATABASE_MIN_CONNS")
	if err != nil {
		return nil, err
	}

	if cfg.DatabaseMaxConns < 1 {
		return nil, fmt.Errorf("DATABASE_MAX_CONNS must be >= 1")
	}
	if cfg.DatabaseMinConns < 0 {
		return nil, fmt.Errorf("DATABASE_MIN_CONNS must be >= 0")
	}
	if cfg.DatabaseMinConns > cfg.DatabaseMaxConns {
		return nil, fmt.Errorf("DATABASE_MIN_CONNS (%d) must not exceed DATABASE_MAX_CONNS (%d)",
			cfg.DatabaseMinConns, cfg.DatabaseMaxConns)
	}

	return cfg, nil
}

func parseDuration(key, def string) (time.Duration, error) {
	v, err := time.ParseDuration(getEnv(key, def))
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	if v <= 0 {
		return 0, fmt.Errorf("%s must be > 0", key)
	}
	return v, nil
}

func parseInt32(s, key string) (int32, error) {
	n, err := strconv.ParseInt(s, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return int32(n), nil
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
