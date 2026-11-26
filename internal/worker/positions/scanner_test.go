package positions_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/mock/gomock"

	"github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/infra/telemetry"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/worker/positions"
)

const (
	staleAfter   = time.Minute
	batchSize    = 100
	checksName   = "tripgo.stale_checks"
	requestsName = "tripgo.position_requests"
	queued       = "queued"
	dropped      = "dropped"
)

type manualTicker struct {
	ticks   chan time.Time
	stopped chan struct{}
}

func (t *manualTicker) Ticks() <-chan time.Time { return t.ticks }
func (t *manualTicker) Stop()                   { close(t.stopped) }

type scanReports struct {
	*telemetry.Metrics
	rows     chan int64
	finished atomic.Int32
}

func (r *scanReports) RecordStaleRows(ctx context.Context, n int64) {
	r.Metrics.RecordStaleRows(ctx, n)
	r.finished.Add(1)
	r.rows <- n
}

func TestScannerBatchesAndStopsOnDrop(t *testing.T) {
	trips := make([]trip.StaleTrip, 250)
	var id byte
	for i := range trips {
		id++
		trips[i] = candidate(id, i+1)
	}
	tests := []struct {
		name                        string
		capacity, reads, rows, size int
		evict                       bool
	}{
		{name: "250 trips in three pages", capacity: 250, reads: 3, rows: 250, size: 250},
		{name: "queue of 50 stops after the first page", capacity: 50, reads: 1, rows: 100, size: 50},
		{name: "eviction also stops the scan", capacity: 1, reads: 1, rows: 100, size: 1, evict: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metrics, reader := newTestMetrics(t)
			gate := candidate(255, 0)
			started := make(chan struct{}, 1)
			release := make(chan struct{})
			pool := startTestPool(t, 1, tt.capacity, testBudget, metrics, func(ctx context.Context, _, id uuid.UUID) error {
				if id == gate.TripID {
					started <- struct{}{}
					return waitForRelease(ctx, release)
				}
				return nil
			})
			if err := metrics.RegisterPositionPool(pool); err != nil {
				t.Fatal(err)
			}
			enqueue(t, pool, gate, positions.Queued)
			receive(t, started)
			if tt.evict {
				enqueue(t, pool, candidate(254, 300), positions.Queued)
			}

			tick := time.Unix(3600, 0)
			repo := NewMockstaleTripRepository(gomock.NewController(t))
			expectPages(repo, tick.Add(-staleAfter), trips, tt.reads)
			var logs bytes.Buffer
			scanner, ticker, reports := startTestScanner(t, repo, pool, metrics, slog.New(slog.NewTextHandler(&logs, nil)))
			ticker.ticks <- tick
			if rows := receive(t, reports.rows); rows != int64(tt.rows) {
				t.Errorf("rows read = %d, want %d", rows, tt.rows)
			}
			stopScanner(t, scanner)
			receive(t, ticker.stopped)
			rm := collect(t, reader)
			assertCount(t, rm, checksName, queued, int64(tt.size))
			assertGauge(t, rm, "tripgo.position_requests.queue.size", int64(tt.size))
			assertGauge(t, rm, "tripgo.position_requests.workers.active", 1)
			rows := findMetric(t, rm, "tripgo.stale_checks.rows_read").(metricdata.Histogram[int64]).DataPoints
			if len(rows) != 1 || rows[0].Count != 1 || rows[0].Sum != int64(tt.rows) {
				t.Errorf("rows histogram = %+v, want one measurement of %d", rows, tt.rows)
			}
			if tt.reads == 1 {
				assertCount(t, rm, checksName, dropped, 1)
				if !strings.Contains(logs.String(), "level=WARN") {
					t.Errorf("missing warning for dropped task: %s", &logs)
				}
			}
			close(release)
			shutdown(t, pool)
		})
	}
}

func TestScannerSerializesTicksAndLinksWorkerSpans(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	traces := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(traces)
	t.Cleanup(func() { otel.SetTracerProvider(prev); _ = traces.Shutdown(context.Background()) })
	metrics, reader := newTestMetrics(t)
	stale, gate, cancelled := candidate(1, 1), candidate(2, 2), candidate(3, 3)
	calls := make(chan uuid.UUID, 6)
	releasePush := make(chan struct{})
	releaseGate := make(chan struct{})
	pool := startTestPool(t, 1, 3, testBudget, metrics, func(ctx context.Context, _, id uuid.UUID) error {
		if ctx.Err() != nil {
			t.Errorf("task context was canceled before request: %v", ctx.Err())
		}
		calls <- id
		if id == gate.TripID {
			if err := waitForRelease(ctx, releaseGate); err != nil {
				return err
			}
			return errors.New("push service unavailable")
		}
		return waitForRelease(ctx, releasePush)
	})
	tick := time.Unix(3600, 0)
	interval := 30 * time.Second
	repo := NewMockstaleTripRepository(gomock.NewController(t))
	firstRead := make(chan struct{})
	releaseRead := make(chan struct{})
	scanner, ticker, reports := startTestScanner(t, repo, pool, metrics, slog.New(slog.DiscardHandler))
	first := repo.EXPECT().ListStaleTrips(gomock.Any(), tick.Add(-staleAfter), nil, batchSize).
		DoAndReturn(func(ctx context.Context, _ time.Time, _ *trip.StaleTripCursor, _ int) ([]trip.StaleTrip, error) {
			close(firstRead)
			return []trip.StaleTrip{stale}, waitForRelease(ctx, releaseRead)
		})
	second := repo.EXPECT().ListStaleTrips(gomock.Any(), tick.Add(interval-staleAfter), nil, batchSize).After(first).
		DoAndReturn(func(context.Context, time.Time, *trip.StaleTripCursor, int) ([]trip.StaleTrip, error) {
			if reports.finished.Load() != 1 {
				t.Error("second scan overlapped with first")
			}
			return []trip.StaleTrip{stale}, nil
		})
	repo.EXPECT().ListStaleTrips(gomock.Any(), tick.Add(2*interval-staleAfter), nil, batchSize).After(second).
		Return([]trip.StaleTrip{stale}, nil)

	ticker.ticks <- tick
	receive(t, firstRead)
	ticker.ticks <- tick.Add(interval)
	close(releaseRead)
	receive(t, reports.rows)
	receive(t, reports.rows)
	if got := receive(t, calls); got != stale.TripID {
		t.Fatalf("first request was for %s, want %s", got, stale.TripID)
	}
	enqueue(t, pool, gate, positions.Queued)
	close(releasePush)
	if got := receive(t, calls); got != gate.TripID {
		t.Fatalf("next request was for %s, want gate %s", got, gate.TripID)
	}
	ticker.ticks <- tick.Add(2 * interval)
	receive(t, reports.rows)
	stopScanner(t, scanner)
	enqueue(t, pool, cancelled, positions.Queued)
	if !pool.Cancel(t.Context(), cancelled.TripID) {
		t.Fatal("expected queued task to be removed")
	}
	close(releaseGate)
	shutdown(t, pool)
	assertCalls(t, calls, stale.TripID)

	rm := collect(t, reader)
	assertCount(t, rm, checksName, queued, 2)
	assertCount(t, rm, checksName, "duplicate", 1)
	assertCount(t, rm, requestsName, "success", 2)
	assertCount(t, rm, requestsName, "error", 1)
	assertCount(t, rm, requestsName, "cancelled", 1)
	durations := findMetric(t, rm, "tripgo.position_requests.duration").(metricdata.Histogram[float64]).DataPoints
	if len(durations) != 2 || durations[0].Count+durations[1].Count != 3 {
		t.Errorf("duration histogram = %+v, want three completed Push calls", durations)
	}
	assertSpanLinks(t, exporter.GetSpans())
}

func assertSpanLinks(t *testing.T, spans tracetest.SpanStubs) {
	t.Helper()
	scans := make(map[trace.SpanID]trace.TraceID)
	var linked int
	for _, span := range spans {
		if span.Name == "positions.scan" {
			scans[span.SpanContext.SpanID()] = span.SpanContext.TraceID()
		}
	}
	for _, span := range spans {
		if span.Name != "positions.process" || len(span.Links) == 0 {
			continue
		}
		link := span.Links[0].SpanContext
		if span.Parent.IsValid() || scans[link.SpanID()] != link.TraceID() {
			t.Errorf("worker span parent/link = %v/%v, want a root span linked to the scan", span.Parent, link)
		}
		linked++
	}
	if len(scans) != 3 || linked != 2 {
		t.Errorf("got %d scans and %d linked tasks, want 3 and 2", len(scans), linked)
	}
}

func expectPages(repo *MockstaleTripRepository, cutoff time.Time, trips []trip.StaleTrip, reads int) {
	var after *trip.StaleTripCursor
	var previous *gomock.Call
	for i := range reads {
		page := trips[i*batchSize : min((i+1)*batchSize, len(trips))]
		call := repo.EXPECT().ListStaleTrips(gomock.Any(), cutoff, after, batchSize).Return(page, nil)
		if previous != nil {
			call.After(previous)
		}
		previous = call
		last := page[len(page)-1]
		after = &trip.StaleTripCursor{EffectiveAt: last.EffectiveAt, TripID: last.TripID}
	}
}

func startTestScanner(t *testing.T, repo *MockstaleTripRepository, pool *positions.Pool, metrics *telemetry.Metrics, log *slog.Logger) (*positions.Scanner, *manualTicker, *scanReports) {
	t.Helper()
	ticker := &manualTicker{ticks: make(chan time.Time, 1), stopped: make(chan struct{})}
	reports := &scanReports{Metrics: metrics, rows: make(chan int64, 4)}
	scanner := positions.NewScanner(t.Context(), repo, pool, reports, ticker, staleAfter, batchSize, log)
	t.Cleanup(func() { stopScanner(t, scanner) })
	return scanner, ticker, reports
}

func stopScanner(t *testing.T, scanner *positions.Scanner) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), testBudget)
	defer cancel()
	if err := scanner.Shutdown(ctx); err != nil {
		t.Fatalf("stop scanner: %v", err)
	}
}

func newTestMetrics(t *testing.T) (*telemetry.Metrics, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	metrics, err := telemetry.NewMetrics(provider.Meter("test"))
	if err != nil {
		t.Fatal(err)
	}
	return metrics, reader
}

func collect(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(t.Context(), &rm); err != nil {
		t.Fatal(err)
	}
	return rm
}

func findMetric(t *testing.T, rm metricdata.ResourceMetrics, name string) metricdata.Aggregation {
	t.Helper()
	for _, scope := range rm.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name == name {
				return metric.Data
			}
		}
	}
	t.Fatalf("missing metric %s", name)
	return nil
}

func assertCount(t *testing.T, rm metricdata.ResourceMetrics, name, result string, want int64) {
	t.Helper()
	for _, point := range findMetric(t, rm, name).(metricdata.Sum[int64]).DataPoints {
		if value, _ := point.Attributes.Value(attribute.Key("result")); value.AsString() == result {
			if point.Value != want {
				t.Errorf("%s[%s] = %d, want %d", name, result, point.Value, want)
			}
			return
		}
	}
	t.Errorf("metric %s is missing result %s", name, result)
}

func assertGauge(t *testing.T, rm metricdata.ResourceMetrics, name string, want int64) {
	t.Helper()
	points := findMetric(t, rm, name).(metricdata.Gauge[int64]).DataPoints
	if len(points) != 1 || points[0].Value != want {
		t.Errorf("%s = %+v, want %d", name, points, want)
	}
}
