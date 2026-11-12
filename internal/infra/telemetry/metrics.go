package telemetry

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Корзины гистограмм длительности, секунды.
var durationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// Metrics хранит инструменты приложения. Имена фиксированы каталогом observability.md.
type Metrics struct {
	meter           metric.Meter
	requestDuration metric.Float64Histogram
	activeRequests  metric.Int64UpDownCounter
	dbDuration      metric.Float64Histogram
	tripsCreated    metric.Int64Counter
	tripsCompleted  metric.Int64Counter
}

func NewMetrics(meter metric.Meter) (*Metrics, error) {
	requestDuration, err := meter.Float64Histogram("http.server.request.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Duration of inbound HTTP requests"),
		metric.WithExplicitBucketBoundaries(durationBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("http.server.request.duration: %w", err)
	}

	activeRequests, err := meter.Int64UpDownCounter("http.server.active_requests",
		metric.WithUnit("{request}"),
		metric.WithDescription("Number of in-flight HTTP requests"),
	)
	if err != nil {
		return nil, fmt.Errorf("http.server.active_requests: %w", err)
	}

	dbDuration, err := meter.Float64Histogram("db.client.operation.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Duration of database client operations"),
		metric.WithExplicitBucketBoundaries(durationBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("db.client.operation.duration: %w", err)
	}

	tripsCreated, err := meter.Int64Counter("tripgo.trips.created",
		metric.WithUnit("{trip}"),
		metric.WithDescription("Created trips"),
	)
	if err != nil {
		return nil, fmt.Errorf("tripgo.trips.created: %w", err)
	}

	tripsCompleted, err := meter.Int64Counter("tripgo.trips.completed",
		metric.WithUnit("{trip}"),
		metric.WithDescription("Completed trips"),
	)
	if err != nil {
		return nil, fmt.Errorf("tripgo.trips.completed: %w", err)
	}

	return &Metrics{
		meter:           meter,
		requestDuration: requestDuration,
		activeRequests:  activeRequests,
		dbDuration:      dbDuration,
		tripsCreated:    tripsCreated,
		tripsCompleted:  tripsCompleted,
	}, nil
}

// routeAttrs: пустой route (маршрут не найден) в метрику не попадает,
// чтобы не плодить серии с произвольными URL.
func routeAttrs(method, route string) []attribute.KeyValue {
	attrs := []attribute.KeyValue{attribute.String("http.request.method", method)}
	if route != "" {
		attrs = append(attrs, attribute.String("http.route", route))
	}
	return attrs
}

func (m *Metrics) AddActiveRequest(ctx context.Context, delta int64, method, route string) {
	m.activeRequests.Add(ctx, delta, metric.WithAttributes(routeAttrs(method, route)...))
}

func (m *Metrics) RecordRequest(ctx context.Context, d time.Duration, method, route string, status int) {
	attrs := append(routeAttrs(method, route), attribute.Int("http.response.status_code", status))
	m.requestDuration.Record(ctx, d.Seconds(), metric.WithAttributes(attrs...))
}

// RecordDBOperation: пустые collection и errType в метрику не попадают.
func (m *Metrics) RecordDBOperation(ctx context.Context, d time.Duration, operation, collection, errType string) {
	attrs := []attribute.KeyValue{attribute.String("db.operation.name", operation)}
	if collection != "" {
		attrs = append(attrs, attribute.String("db.collection.name", collection))
	}
	if errType != "" {
		attrs = append(attrs, attribute.String("error.type", errType))
	}
	m.dbDuration.Record(ctx, d.Seconds(), metric.WithAttributes(attrs...))
}

// ActiveTripsSource отдаёт текущее число активных поездок (значение берётся из БД).
type ActiveTripsSource interface {
	CountActive(ctx context.Context) (int64, error)
}

// RegisterActiveTrips регистрирует gauge tripgo.trips.active. Значение читается
// из источника в момент сбора метрик, а не хранится в памяти: так оно верно после
// рестарта и при нескольких репликах. Если запрос не удался, точка не
// отправляется (лучше пропуск, чем ложный ноль).
func (m *Metrics) RegisterActiveTrips(src ActiveTripsSource) error {
	_, err := m.meter.Int64ObservableGauge("tripgo.trips.active",
		metric.WithUnit("{trip}"),
		metric.WithDescription("Trips currently in active status"),
		metric.WithInt64Callback(func(ctx context.Context, o metric.Int64Observer) error {
			n, err := src.CountActive(ctx)
			if err != nil {
				return fmt.Errorf("observe active trips: %w", err)
			}
			o.Observe(n)
			return nil
		}),
	)
	if err != nil {
		return fmt.Errorf("tripgo.trips.active: %w", err)
	}
	return nil
}

func (m *Metrics) TripCreated(ctx context.Context) {
	m.tripsCreated.Add(ctx, 1)
}

func (m *Metrics) TripCompleted(ctx context.Context) {
	m.tripsCompleted.Add(ctx, 1)
}
