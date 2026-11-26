package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/TemirlanKoybaev/tripgoavito.git/internal/config"
)

const (
	checkIntervalEnv = "POSITION_CHECK_INTERVAL"
	staleAfterEnv    = "POSITION_STALE_AFTER"
	workersEnv       = "POSITION_WORKERS"
	queueSizeEnv     = "POSITION_QUEUE_SIZE"
	batchSizeEnv     = "POSITION_SCAN_BATCH_SIZE"
	pushURLEnv       = "PUSH_HTTP_URL"
	pushTimeoutEnv   = "PUSH_REQUEST_TIMEOUT"
	defaultPushURL   = "http://push.example.test:8090"
)

type positionSettings struct {
	checkInterval time.Duration
	staleAfter    time.Duration
	workers       int
	queueSize     int
	batchSize     int
	pushURL       string
	pushTimeout   time.Duration
}

func TestPositionSettings(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want positionSettings
	}{
		{
			name: "defaults with the push service address",
			want: positionSettings{
				checkInterval: 30 * time.Second,
				staleAfter:    time.Minute,
				workers:       4,
				queueSize:     100,
				batchSize:     100,
				pushURL:       defaultPushURL,
				pushTimeout:   time.Second,
			},
		},
		{
			name: "environment overrides",
			env: map[string]string{
				checkIntervalEnv: "3s",
				staleAfterEnv:    "2m",
				workersEnv:       "6",
				queueSizeEnv:     "42",
				batchSizeEnv:     "25",
				pushURLEnv:       "https://push.example.test:8443/gateway",
				pushTimeoutEnv:   "1250ms",
			},
			want: positionSettings{
				checkInterval: 3 * time.Second,
				staleAfter:    2 * time.Minute,
				workers:       6,
				queueSize:     42,
				batchSize:     25,
				pushURL:       "https://push.example.test:8443/gateway",
				pushTimeout:   1250 * time.Millisecond,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setPositionTestEnv(t)
			for key, val := range tt.env {
				t.Setenv(key, val)
			}
			cfg, err := config.Load()
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			assertPositionSettings(t, cfg, tt.want)
			if cfg.HTTPAddr != "127.0.0.1:8088" || cfg.DatabaseMinConns != 0 {
				t.Error("existing HTTP and PostgreSQL settings were not kept")
			}
		})
	}
}

func assertPositionSettings(t *testing.T, cfg *config.Config, want positionSettings) {
	t.Helper()
	var pushURL string
	if cfg.PushHTTPURL != nil {
		pushURL = cfg.PushHTTPURL.String()
	}
	for _, check := range []struct {
		key       string
		got, want any
	}{
		{checkIntervalEnv, cfg.PositionCheckInterval, want.checkInterval},
		{staleAfterEnv, cfg.PositionStaleAfter, want.staleAfter},
		{workersEnv, cfg.PositionWorkers, want.workers},
		{queueSizeEnv, cfg.PositionQueueSize, want.queueSize},
		{batchSizeEnv, cfg.PositionScanBatchSize, want.batchSize},
		{pushURLEnv, pushURL, want.pushURL},
		{pushTimeoutEnv, cfg.PushRequestTimeout, want.pushTimeout},
	} {
		if check.got != check.want {
			t.Errorf("%s = %v, want %v", check.key, check.got, check.want)
		}
	}
}

func TestPositionSettingsRejectInvalidValues(t *testing.T) {
	tests := []struct {
		key   string
		value string
	}{
		{checkIntervalEnv, "0s"},
		{staleAfterEnv, "soon"},
		{workersEnv, "-2"},
		{queueSizeEnv, "0"},
		{batchSizeEnv, "2.5"},
		{pushTimeoutEnv, "-1s"},
		{pushURLEnv, ""},
		{pushURLEnv, "push.example.test:8080"},
		{pushURLEnv, "ftp://push.example.test"},
		{pushURLEnv, "http://:8080"},
		{pushURLEnv, "http://[::1"},
	}
	for _, tt := range tests {
		t.Run(tt.key+"="+tt.value, func(t *testing.T) {
			setPositionTestEnv(t)
			t.Setenv(tt.key, tt.value)
			cfg, err := config.Load()
			if cfg != nil || err == nil || !strings.Contains(err.Error(), tt.key) {
				t.Fatalf("Load() = config set: %v, error: %v, want validation error for %s", cfg != nil, err, tt.key)
			}
		})
	}
}

func setPositionTestEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://localhost/tripgo")
	t.Setenv("DATABASE_MIN_CONNS", "0")
	t.Setenv("HTTP_ADDR", "127.0.0.1:8088")
	for _, key := range []string{
		checkIntervalEnv, staleAfterEnv, workersEnv, queueSizeEnv, batchSizeEnv, pushTimeoutEnv,
	} {
		t.Setenv(key, "")
	}
	t.Setenv(pushURLEnv, defaultPushURL)
}
