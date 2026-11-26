package telemetry

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type positionInstruments struct {
	saved           metric.Int64Counter
	checks          metric.Int64Counter
	rowsRead        metric.Int64Histogram
	requests        metric.Int64Counter
	requestDuration metric.Float64Histogram
}

func newPositionInstruments(meter metric.Meter) (positionInstruments, error) {
	var p positionInstruments
	var err error
	p.saved, err = meter.Int64Counter("tripgo.positions.saved", metric.WithUnit("{position}"))
	if err != nil {
		return p, fmt.Errorf("tripgo.positions.saved: %w", err)
	}
	p.checks, err = meter.Int64Counter("tripgo.stale_checks", metric.WithUnit("{trip}"))
	if err != nil {
		return p, fmt.Errorf("tripgo.stale_checks: %w", err)
	}
	p.rowsRead, err = meter.Int64Histogram("tripgo.stale_checks.rows_read", metric.WithUnit("{row}"),
		metric.WithExplicitBucketBoundaries(0, 10, 25, 50, 100, 250, 500, 1000, 5000),
	)
	if err != nil {
		return p, fmt.Errorf("tripgo.stale_checks.rows_read: %w", err)
	}
	p.requests, err = meter.Int64Counter("tripgo.position_requests", metric.WithUnit("{request}"))
	if err != nil {
		return p, fmt.Errorf("tripgo.position_requests: %w", err)
	}
	p.requestDuration, err = meter.Float64Histogram("tripgo.position_requests.duration", metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(durationBuckets...),
	)
	if err != nil {
		return p, fmt.Errorf("tripgo.position_requests.duration: %w", err)
	}
	return p, nil
}

type PositionPoolSource interface {
	Stats() (queued, active int64)
}

func (m *Metrics) RegisterPositionPool(src PositionPoolSource) error {
	queue, err := m.meter.Int64ObservableGauge("tripgo.position_requests.queue.size", metric.WithUnit("{task}"))
	if err != nil {
		return fmt.Errorf("tripgo.position_requests.queue.size: %w", err)
	}
	workers, err := m.meter.Int64ObservableGauge("tripgo.position_requests.workers.active", metric.WithUnit("{worker}"))
	if err != nil {
		return fmt.Errorf("tripgo.position_requests.workers.active: %w", err)
	}
	_, err = m.meter.RegisterCallback(func(_ context.Context, observer metric.Observer) error {
		queued, active := src.Stats()
		observer.ObserveInt64(queue, queued)
		observer.ObserveInt64(workers, active)
		return nil
	}, queue, workers)
	if err != nil {
		return fmt.Errorf("observe position pool: %w", err)
	}
	return nil
}

func (m *Metrics) PositionSaved(ctx context.Context) {
	m.positions.saved.Add(ctx, 1)
}

func (m *Metrics) StaleCheck(ctx context.Context, result string) {
	m.positions.checks.Add(ctx, 1, metric.WithAttributes(attribute.String("result", result)))
}

func (m *Metrics) RecordStaleRows(ctx context.Context, rows int64) {
	m.positions.rowsRead.Record(ctx, rows)
}

func (m *Metrics) PositionRequest(ctx context.Context, result string) {
	m.positions.requests.Add(ctx, 1, metric.WithAttributes(attribute.String("result", result)))
}

func (m *Metrics) RecordPositionRequestDuration(ctx context.Context, duration time.Duration, result string) {
	m.positions.requestDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(attribute.String("result", result)))
}
