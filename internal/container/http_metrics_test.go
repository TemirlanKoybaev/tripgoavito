package container

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/TemirlanKoybaev/tripgoavito.git/api"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/http/middleware"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/http/router"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/infra/logger"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/infra/telemetry"
)

const (
	routeTemplate         = "/api/v1/trips/{tripId}"
	requestDurationMetric = "http.server.request.duration"
	routeAttribute        = "http.route"
	statusAttribute       = "http.response.status_code"
)

type metricsEnv struct {
	reader  *sdkmetric.ManualReader
	handler http.Handler
}

func newHTTPMetrics(t *testing.T) (*sdkmetric.ManualReader, *telemetry.Metrics) {
	t.Helper()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	metrics, err := telemetry.NewMetrics(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewMetrics: %v", err)
	}
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	return reader, metrics
}

func newMetricsEnv(t *testing.T, onRequest func()) *metricsEnv {
	t.Helper()
	reader, metrics := newHTTPMetrics(t)
	mux := chi.NewRouter()
	mux.With(middleware.TagRoute).Get(routeTemplate, func(w http.ResponseWriter, _ *http.Request) {
		if onRequest != nil {
			onRequest()
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware.Tracing(middleware.Metrics(metrics)(mux))
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

	duration := findMetric(rm, requestDurationMetric)
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
	if v, _ := attrValue(dp.Attributes, routeAttribute); v.AsString() != routeTemplate {
		t.Errorf("http.route = %q, want %q", v.AsString(), routeTemplate)
	}
	if v, _ := attrValue(dp.Attributes, "http.request.method"); v.AsString() != http.MethodGet {
		t.Errorf("http.request.method = %q, want GET", v.AsString())
	}
	if v, _ := attrValue(dp.Attributes, statusAttribute); v.AsInt64() != http.StatusOK {
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

	duration := findMetric(env.collect(t), requestDurationMetric)
	if duration == nil {
		t.Fatal("http.server.request.duration is not recorded")
	}
	points := duration.Data.(metricdata.Histogram[float64]).DataPoints
	if len(points) != 1 {
		t.Fatalf("data points = %d, want 1 (unknown URLs must not create series)", len(points))
	}
	if _, ok := attrValue(points[0].Attributes, routeAttribute); ok {
		t.Error("unmatched request must not carry http.route")
	}
	if v, _ := attrValue(points[0].Attributes, statusAttribute); v.AsInt64() != http.StatusNotFound {
		t.Errorf("status = %d, want 404", v.AsInt64())
	}
	if got := activeNow(t, env); got != 0 {
		t.Errorf("active_requests = %d, want 0", got)
	}
}

func TestRouterTagsInvalidTripID(t *testing.T) {
	tests := []struct {
		method   string
		path     string
		template string
	}{
		{http.MethodGet, "/api/v1/trips/not-a-uuid", routeTemplate},
		{http.MethodPost, "/api/v1/trips/not-a-uuid/finish", routeTemplate + "/finish"},
		{http.MethodPost, "/api/v1/trips/not-a-uuid/positions", routeTemplate + "/positions"},
		{http.MethodGet, "/api/v1/trips/not-a-uuid/positions", routeTemplate + "/positions"},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.template, func(t *testing.T) {
			exporter := tracetest.NewInMemoryExporter()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
			prev := otel.GetTracerProvider()
			otel.SetTracerProvider(provider)
			t.Cleanup(func() {
				otel.SetTracerProvider(prev)
				_ = provider.Shutdown(context.Background())
			})

			reader, metrics := newHTTPMetrics(t)
			var logs bytes.Buffer
			lg := logger.NewWithWriter(&logs, "info", "trip-service")
			handler := withHTTPMiddleware(router.New(nil, nil, nil, nil, nil, lg).Handler(), metrics, lg)

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest(tt.method, tt.path, http.NoBody))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", w.Code)
			}
			var problem api.Problem
			if err := json.Unmarshal(w.Body.Bytes(), &problem); err != nil || problem.Code != "invalid_request" {
				t.Errorf("problem = %+v, decode error = %v", problem, err)
			}

			var rec struct {
				Message string `json:"msg"`
				Route   string `json:"route"`
				Status  int    `json:"status"`
			}
			if err := json.NewDecoder(&logs).Decode(&rec); err != nil {
				t.Fatalf("decode request log: %v", err)
			}
			if rec.Message != "request handled" || rec.Route != tt.template || rec.Status != http.StatusBadRequest {
				t.Errorf("request log = %+v, want route %q and status 400", rec, tt.template)
			}

			env := metricsEnv{reader: reader}
			duration := findMetric(env.collect(t), requestDurationMetric)
			if duration == nil {
				t.Fatal("http.server.request.duration is not recorded")
			}
			points := duration.Data.(metricdata.Histogram[float64]).DataPoints
			if len(points) != 1 || points[0].Count != 1 {
				t.Fatalf("duration data points = %+v, want one measurement", points)
			}
			attrs := points[0].Attributes
			if route, _ := attrValue(attrs, routeAttribute); route.AsString() != tt.template {
				t.Errorf("metric http.route = %q, want %q", route.AsString(), tt.template)
			}
			if status, _ := attrValue(attrs, statusAttribute); status.AsInt64() != http.StatusBadRequest {
				t.Errorf("metric status = %d, want 400", status.AsInt64())
			}

			spans := exporter.GetSpans()
			if len(spans) != 1 {
				t.Fatalf("spans = %d, want 1", len(spans))
			}
			span := spans[0]
			if route, _ := attrValue(attribute.NewSet(span.Attributes...), routeAttribute); route.AsString() != tt.template {
				t.Errorf("span http.route = %q, want %q", route.AsString(), tt.template)
			}
			if want := tt.method + " " + tt.template; span.Name != want {
				t.Errorf("span name = %q, want %q", span.Name, want)
			}
		})
	}
}
