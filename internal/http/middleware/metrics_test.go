package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/TemirlanKoybaev/tripgoavito.git/internal/http/middleware"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/infra/telemetry"
)

const routeTemplate = "/api/v1/trips/{tripId}"

type metricsEnv struct {
	reader  *sdkmetric.ManualReader
	handler http.Handler
}

// newMetricsEnv собирает цепочку как в проде: Tracing → Metrics → chi с TagRoute.
func newMetricsEnv(t *testing.T, onRequest func()) *metricsEnv {
	t.Helper()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	metrics, err := telemetry.NewMetrics(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewMetrics: %v", err)
	}

	router := chi.NewRouter()
	router.With(middleware.TagRoute).Get(routeTemplate, func(w http.ResponseWriter, _ *http.Request) {
		if onRequest != nil {
			onRequest()
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware.Tracing(middleware.Metrics(metrics)(router))
	return &metricsEnv{reader: reader, handler: handler}
}

func (e *metricsEnv) get(path string) {
	e.handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, http.NoBody))
}

func (e *metricsEnv) collect(t *testing.T) metricdata.ResourceMetrics {
	t.Helper()

	var rm metricdata.ResourceMetrics
	if err := e.reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	return rm
}

func findMetric(rm metricdata.ResourceMetrics, name string) *metricdata.Metrics {
	for _, sm := range rm.ScopeMetrics {
		for i := range sm.Metrics {
			if sm.Metrics[i].Name == name {
				return &sm.Metrics[i]
			}
		}
	}
	return nil
}

func attrValue(set attribute.Set, key string) (attribute.Value, bool) {
	return set.Value(attribute.Key(key))
}

func activeNow(t *testing.T, e *metricsEnv) int64 {
	t.Helper()

	m := findMetric(e.collect(t), "http.server.active_requests")
	if m == nil {
		return 0
	}
	var total int64
	for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
		total += dp.Value
	}
	return total
}

func TestMetricsRecordsRouteTemplate(t *testing.T) {
	var env *metricsEnv
	var activeInside int64
	env = newMetricsEnv(t, func() { activeInside = activeNow(t, env) })

	// Два разных id: если бы в метрику попадал URL, серий было бы две.
	env.get("/api/v1/trips/11111111-1111-1111-1111-111111111111")
	env.get("/api/v1/trips/22222222-2222-2222-2222-222222222222")

	rm := env.collect(t)

	duration := findMetric(rm, "http.server.request.duration")
	if duration == nil {
		t.Fatal("http.server.request.duration is not recorded")
	}
	points := duration.Data.(metricdata.Histogram[float64]).DataPoints
	if len(points) != 1 {
		t.Fatalf("data points = %d, want 1 (route must be a template, not a URL)", len(points))
	}
	dp := points[0]
	if dp.Count != 2 {
		t.Errorf("count = %d, want 2", dp.Count)
	}
	if v, _ := attrValue(dp.Attributes, "http.route"); v.AsString() != routeTemplate {
		t.Errorf("http.route = %q, want %q", v.AsString(), routeTemplate)
	}
	if v, _ := attrValue(dp.Attributes, "http.request.method"); v.AsString() != http.MethodGet {
		t.Errorf("http.request.method = %q, want GET", v.AsString())
	}
	if v, _ := attrValue(dp.Attributes, "http.response.status_code"); v.AsInt64() != http.StatusOK {
		t.Errorf("http.response.status_code = %d, want 200", v.AsInt64())
	}

	if activeInside != 1 {
		t.Errorf("active_requests inside the handler = %d, want 1", activeInside)
	}
	if got := activeNow(t, env); got != 0 {
		t.Errorf("active_requests after the requests = %d, want 0", got)
	}
}

func TestMetricsUnmatchedRouteHasNoRouteLabel(t *testing.T) {
	env := newMetricsEnv(t, nil)

	env.get("/no/such/path/1")
	env.get("/no/such/path/2")

	duration := findMetric(env.collect(t), "http.server.request.duration")
	if duration == nil {
		t.Fatal("http.server.request.duration is not recorded")
	}
	points := duration.Data.(metricdata.Histogram[float64]).DataPoints
	if len(points) != 1 {
		t.Fatalf("data points = %d, want 1 (unknown URLs must not create series)", len(points))
	}
	if _, ok := attrValue(points[0].Attributes, "http.route"); ok {
		t.Error("unmatched request must not carry http.route")
	}
	if v, _ := attrValue(points[0].Attributes, "http.response.status_code"); v.AsInt64() != http.StatusNotFound {
		t.Errorf("status = %d, want 404", v.AsInt64())
	}
	if got := activeNow(t, env); got != 0 {
		t.Errorf("active_requests = %d, want 0", got)
	}
}
