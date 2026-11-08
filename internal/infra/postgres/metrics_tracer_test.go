package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
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
	selectOne  = "SELECT 1 FROM trips"
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
		{"timeout", selectOne, fmt.Errorf("wrap: %w", context.DeadlineExceeded), opSelect, tableTrips, "timeout"},
		{"other error gives type", selectOne, errors.New("boom"), opSelect, tableTrips, "*errors.errorString"},
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

// error.type и db.collection.name кладутся на спан запроса (его создаёт otelpgx),
// статус ошибки ставит сам otelpgx.
func TestMetricsTracerMarksSpan(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	tracer := metricsTracer{recorder: &recorderSpy{}}

	ctx, span := tp.Tracer("test").Start(context.Background(), "SELECT")
	ctx = tracer.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{SQL: selectOne})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{Err: &pgconn.PgError{Code: "23505"}})
	span.End()

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	got := map[string]string{}
	for _, a := range spans[0].Attributes {
		got[string(a.Key)] = a.Value.AsString()
	}
	if got["error.type"] != "23505" {
		t.Errorf("error.type = %q, want 23505", got["error.type"])
	}
	if got["db.collection.name"] != tableTrips {
		t.Errorf("db.collection.name = %q, want %s", got["db.collection.name"], tableTrips)
	}
}

func TestMetricsTracerMarksPrepareSpan(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	tracer := metricsTracer{recorder: &recorderSpy{}}

	ctx, span := tp.Tracer("test").Start(context.Background(), "INSERT")
	ctx = tracer.TracePrepareStart(ctx, nil, pgx.TracePrepareStartData{SQL: "INSERT INTO trips (id) VALUES ($1)"})
	tracer.TracePrepareEnd(ctx, nil, pgx.TracePrepareEndData{})
	span.End()

	var got string
	for _, a := range exporter.GetSpans()[0].Attributes {
		if string(a.Key) == "db.collection.name" {
			got = a.Value.AsString()
		}
	}
	if got != tableTrips {
		t.Errorf("db.collection.name = %q, want %s", got, tableTrips)
	}
}
