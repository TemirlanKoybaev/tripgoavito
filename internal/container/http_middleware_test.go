package container

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/TemirlanKoybaev/tripgoavito.git/api"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/http/router"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/infra/logger"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/infra/telemetry"
)

const (
	httpStatusAttribute = "http.response.status_code"
	panicMessage        = "test trip handler panic"
)

type panickingGet struct{}

func (panickingGet) Handle(http.ResponseWriter, *http.Request, api.TripId) error {
	panic(panicMessage)
}

func TestHTTPMiddlewareRecoversPanicWithTrace(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	traces := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(traces)
	t.Cleanup(func() {
		otel.SetTracerProvider(prev)
		_ = traces.Shutdown(context.Background())
	})

	reader := sdkmetric.NewManualReader()
	meters := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = meters.Shutdown(context.Background()) })
	metrics, err := telemetry.NewMetrics(meters.Meter("test"))
	if err != nil {
		t.Fatalf("create metrics: %v", err)
	}

	var logs bytes.Buffer
	lg := logger.NewWithWriter(&logs, "info", "trip-service")
	handler := withHTTPMiddleware(router.New(nil, panickingGet{}, nil, nil, lg).Handler(), metrics, lg)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/trips/"+uuid.NewString(), http.NoBody)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError || w.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("response = %d %s, want 500 application/problem+json", w.Code, w.Header().Get("Content-Type"))
	}
	var problem api.Problem
	if err := json.Unmarshal(w.Body.Bytes(), &problem); err != nil || problem.Code != "internal_error" {
		t.Errorf("problem = %+v, decode error = %v", problem, err)
	}

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	span := spans[0]
	attrs := attribute.NewSet(span.Attributes...)
	if status, _ := attrs.Value(attribute.Key(httpStatusAttribute)); status.AsInt64() != http.StatusInternalServerError || span.Status.Code != codes.Error {
		t.Errorf("HTTP span status = %d, %v, want 500, Error", status.AsInt64(), span.Status.Code)
	}
	if errorType, _ := attrs.Value("error.type"); errorType.AsString() != "panic" {
		t.Errorf("span error.type = %q, want panic", errorType.AsString())
	}

	assertPanicLog(t, &logs, span.SpanContext.TraceID().String(), span.SpanContext.SpanID().String())
	assertHTTPMetricStatus(t, reader)
}

func assertPanicLog(t *testing.T, logs *bytes.Buffer, traceID, spanID string) {
	t.Helper()
	dec := json.NewDecoder(logs)
	for dec.More() {
		var rec struct {
			Message string `json:"msg"`
			Panic   string `json:"panic"`
			Stack   string `json:"stack"`
			TraceID string `json:"trace_id"`
			SpanID  string `json:"span_id"`
		}
		if err := dec.Decode(&rec); err != nil {
			t.Fatalf("decode log: %v", err)
		}
		if rec.Message != "panic recovered" {
			continue
		}
		if rec.Panic != panicMessage || !strings.Contains(rec.Stack, "panickingGet.Handle") {
			t.Errorf("panic log = %+v, want handler stack and original panic", rec)
		}
		if rec.TraceID != traceID || rec.SpanID != spanID {
			t.Errorf("panic log trace_id/span_id = %q/%q, want HTTP span %q/%q", rec.TraceID, rec.SpanID, traceID, spanID)
		}
		return
	}
	t.Error("panic recovered log is missing")
}

func assertHTTPMetricStatus(t *testing.T, reader *sdkmetric.ManualReader) {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(t.Context(), &rm); err != nil {
		t.Fatalf("collect HTTP metrics: %v", err)
	}
	for _, scope := range rm.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name != "http.server.request.duration" {
				continue
			}
			points := metric.Data.(metricdata.Histogram[float64]).DataPoints
			if len(points) != 1 || points[0].Count != 1 {
				t.Fatalf("HTTP duration data points = %+v, want one measurement", points)
			}
			if status, _ := points[0].Attributes.Value(attribute.Key(httpStatusAttribute)); status.AsInt64() != http.StatusInternalServerError {
				t.Errorf("HTTP duration metric status = %d, want 500", status.AsInt64())
			}
			return
		}
	}
	t.Error("http.server.request.duration metric is missing")
}
