package middleware_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/TemirlanKoybaev/tripgoavito.git/api"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/http/health"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/http/middleware"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/http/router"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/http/trip/get"
)

type failingDB struct{ err error }

func (db failingDB) Get(context.Context, uuid.UUID) (*trip.Trip, error) { return nil, db.err }

func (db failingDB) Ping(context.Context) error { return db.err }

type discardMetrics struct{}

func (discardMetrics) AddActiveRequest(context.Context, int64, string, string) {}
func (discardMetrics) RecordRequest(context.Context, time.Duration, string, string, int) {
}

func TestRouterReportsOriginal5xxCause(t *testing.T) {
	cause := fmt.Errorf("query trip: %w", errors.New("database connection lost"))
	db := failingDB{err: cause}

	tests := []struct {
		path        string
		status      int
		contentType string
		errorType   string
	}{
		{"/api/v1/trips/" + uuid.NewString(), http.StatusInternalServerError, "application/problem+json", codeInternalError},
		{"/ready", http.StatusServiceUnavailable, "application/json", "db_unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.errorType, func(t *testing.T) {
			exporter := tracetest.NewInMemoryExporter()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
			prev := otel.GetTracerProvider()
			otel.SetTracerProvider(provider)
			t.Cleanup(func() { otel.SetTracerProvider(prev) })

			logger, logs := newTestLogger()
			handler := router.New(nil, get.New(db), nil, nil, health.New(db, time.Second), logger).Handler()
			handler = middleware.Recover(logger)(handler)
			handler = middleware.Metrics(discardMetrics{})(handler)
			handler = middleware.Logging(logger)(handler)
			handler = middleware.Tracing(handler)

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tt.path, http.NoBody))
			if w.Code != tt.status || w.Header().Get("Content-Type") != tt.contentType {
				t.Fatalf("response = %d %s, want %d %s", w.Code, w.Header().Get("Content-Type"), tt.status, tt.contentType)
			}
			if strings.Contains(w.Body.String(), cause.Error()) {
				t.Errorf("response exposed cause: %s", w.Body.String())
			}
			if tt.status == http.StatusInternalServerError {
				var problem api.Problem
				if err := json.Unmarshal(w.Body.Bytes(), &problem); err != nil {
					t.Fatalf("decode problem: %v", err)
				}
				if problem.Code != tt.errorType || int(problem.Status) != tt.status || problem.Instance == nil || *problem.Instance != tt.path {
					t.Errorf("unexpected problem: %+v", problem)
				}
			} else {
				var readiness api.HealthResponse
				if err := json.Unmarshal(w.Body.Bytes(), &readiness); err != nil || readiness.Status != api.Unavailable {
					t.Errorf("readiness response = %+v, decode error = %v", readiness, err)
				}
			}

			rec := findLog(parseLog(t, logs), requestHandled)
			if rec["level"] != slog.LevelError.String() || rec["error"] != cause.Error() {
				t.Errorf("request log = %v, want ERROR with %q", rec, cause)
			}
			spans := exporter.GetSpans()
			if len(spans) != 1 || spans[0].Status.Code != codes.Error {
				t.Fatalf("want one span with error status, got %+v", spans)
			}
			attrs := spans[0].Attributes
			var errorType string
			for _, attr := range attrs {
				if attr.Key == "error.type" {
					errorType = attr.Value.AsString()
				}
			}
			if errorType != tt.errorType {
				t.Errorf("span error.type = %q, want %q", errorType, tt.errorType)
			}
			events := spans[0].Events
			if len(events) != 1 || events[0].Name != "exception" {
				t.Fatalf("span events = %+v, want an exception", events)
			}
			for _, attr := range events[0].Attributes {
				if attr.Key == "exception.message" && attr.Value.AsString() == cause.Error() {
					return
				}
			}
			t.Errorf("span exception = %+v, want %q", events[0], cause)
		})
	}
}

type failedResponseWriter struct {
	*httptest.ResponseRecorder
	writeErr error
	headers  []int
}

func (w *failedResponseWriter) WriteHeader(status int) {
	w.headers = append(w.headers, status)
	w.ResponseRecorder.WriteHeader(status)
}

func (w *failedResponseWriter) Write([]byte) (int, error) { return 0, w.writeErr }

func TestRouterLogsWriteFailureWithoutReplacingResponse(t *testing.T) {
	for _, logMessage := range []string{requestHandled, "response error"} {
		t.Run(logMessage, func(t *testing.T) {
			logger, logs := newTestLogger()
			handler := router.New(nil, nil, nil, nil, health.New(nil, time.Second), logger).Handler()
			if logMessage == requestHandled {
				handler = middleware.Logging(logger)(handler)
			}
			req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
			cause := errors.New("write failed")
			w := &failedResponseWriter{ResponseRecorder: httptest.NewRecorder(), writeErr: cause}
			handler.ServeHTTP(w, req)

			if len(w.headers) != 1 || w.headers[0] != http.StatusOK || w.Header().Get("Content-Type") != "application/json" {
				t.Errorf("unexpected response statuses or content type: %v %v", w.headers, w.Header())
			}
			rec := findLog(parseLog(t, logs), logMessage)
			if rec["level"] != "WARN" || rec["error"] != cause.Error() {
				t.Errorf("request log = %v, want WARN with %q", rec, cause)
			}
		})
	}
}
