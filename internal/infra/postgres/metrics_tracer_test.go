package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type recorderSpy struct {
	operation, collection, errType string
	calls                          int
}

func (s *recorderSpy) RecordDBOperation(_ context.Context, _ time.Duration, operation, collection, errType string) {
	s.operation, s.collection, s.errType = operation, collection, errType
	s.calls++
}

const (
	opSelect   = "SELECT"
	tableTrips = "trips"
)

func TestMetricsTracer(t *testing.T) {
	tests := []struct {
		name       string
		sql        string
		err        error
		operation  string
		collection string
		errType    string
	}{
		{"select", `SELECT id FROM trips WHERE id = $1 FOR UPDATE`, nil, opSelect, tableTrips, ""},
		{"insert", `INSERT INTO trip_status_history (trip_id) VALUES ($1)`, nil, "INSERT", "trip_status_history", ""},
		{"update", `UPDATE trips SET status = $1 WHERE id = $2`, nil, "UPDATE", tableTrips, ""},
		{"commit has no table", `commit`, nil, "COMMIT", "", ""},
		{"pg error gives sqlstate", `INSERT INTO trips (id) VALUES ($1)`, &pgconn.PgError{Code: "23505"}, "INSERT", tableTrips, "23505"},
		{"timeout", `SELECT 1 FROM trips`, fmt.Errorf("wrap: %w", context.DeadlineExceeded), opSelect, tableTrips, "timeout"},
		{"other error gives type", `SELECT 1 FROM trips`, errors.New("boom"), opSelect, tableTrips, "*errors.errorString"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spy := &recorderSpy{}
			tracer := metricsTracer{recorder: spy}

			ctx := tracer.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: tt.sql})
			tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{Err: tt.err})

			if spy.calls != 1 {
				t.Fatalf("calls = %d, want 1", spy.calls)
			}
			if spy.operation != tt.operation || spy.collection != tt.collection || spy.errType != tt.errType {
				t.Errorf("got (%q, %q, %q), want (%q, %q, %q)",
					spy.operation, spy.collection, spy.errType, tt.operation, tt.collection, tt.errType)
			}
		})
	}
}
