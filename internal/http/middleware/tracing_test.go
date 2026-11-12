package middleware_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/http/domainerror"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/http/middleware"
)

func TestTracingMarksSpan(t *testing.T) {
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantStatus codes.Code
		wantType   string
	}{
		{
			name:       "success has no error",
			handler:    func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) },
			wantStatus: codes.Unset,
		},
		{
			name: "client error has type but no error status",
			handler: func(w http.ResponseWriter, r *http.Request) {
				domainerror.Write(w, r, trip.ErrTripNotFound)
			},
			wantStatus: codes.Unset,
			wantType:   "trip_not_found",
		},
		{
			name: "conflict is a client error too",
			handler: func(w http.ResponseWriter, r *http.Request) {
				domainerror.Write(w, r, trip.ErrDriverBusy)
			},
			wantStatus: codes.Unset,
			wantType:   "driver_busy",
		},
		{
			name: "internal error sets error status and low-cardinality type",
			handler: func(w http.ResponseWriter, r *http.Request) {
				domainerror.Write(w, r, errors.New("pq: connection refused to 10.0.0.5"))
			},
			wantStatus: codes.Error,
			wantType:   codeInternalError,
		},
		{
			name:       "status without domain code falls back to status number",
			handler:    func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) },
			wantStatus: codes.Unset,
			wantType:   "404",
		},
		{
			name:       "panic is an error",
			handler:    func(http.ResponseWriter, *http.Request) { panic("boom") },
			wantStatus: codes.Error,
			wantType:   "panic",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exporter := tracetest.NewInMemoryExporter()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
			prev := otel.GetTracerProvider()
			otel.SetTracerProvider(tp)
			t.Cleanup(func() { otel.SetTracerProvider(prev) })

			h := middleware.Tracing(tt.handler)
			func() {
				defer func() { _ = recover() }()
				h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/trips/1", http.NoBody))
			}()

			spans := exporter.GetSpans()
			if len(spans) != 1 {
				t.Fatalf("spans = %d, want 1", len(spans))
			}
			span := spans[0]

			if span.Status.Code != tt.wantStatus {
				t.Errorf("status = %v, want %v", span.Status.Code, tt.wantStatus)
			}
			var gotType string
			for _, a := range span.Attributes {
				if string(a.Key) == "error.type" {
					gotType = a.Value.AsString()
				}
			}
			if gotType != tt.wantType {
				t.Errorf("error.type = %q, want %q", gotType, tt.wantType)
			}
			// Причина 5xx пишется в событие спана, но не в статус и не в атрибут.
			if tt.wantType == codeInternalError && len(span.Events) == 0 {
				t.Error("5xx must record the cause as a span event")
			}
		})
	}
}
