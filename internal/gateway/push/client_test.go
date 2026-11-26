package push_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/TemirlanKoybaev/tripgoavito.git/internal/gateway/push"
)

func TestRequestPositionSendsContractPayload(t *testing.T) {
	driverID, tripID := uuid.New(), uuid.New()
	type payload struct {
		RequestID   string            `json:"request_id"`
		RecipientID uuid.UUID         `json:"recipient_id"`
		Kind        string            `json:"kind"`
		Data        map[string]string `json:"data"`
	}
	requestIDs := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/gateway/api/v1/push" {
			t.Errorf("request = %s %s, want POST /gateway/api/v1/push", r.Method, r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content type = %q, want application/json", ct)
		}
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		var body payload
		if err := dec.Decode(&body); err != nil {
			t.Errorf("decode push request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if body.RequestID == "" || body.RecipientID != driverID || body.Kind != "REQUEST_POSITION" ||
			len(body.Data) != 1 || body.Data["trip_id"] != tripID.String() {
			t.Errorf("push request = %+v, want REQUEST_POSITION for driver %s and trip %s", body, driverID, tripID)
		}
		select {
		case requestIDs <- body.RequestID:
		default:
			t.Error("client sent an extra request")
		}
		w.WriteHeader(http.StatusAccepted)
		if _, err := io.WriteString(w, `{"message_id":"sample","accepted_at":"2026-11-27T00:00:00Z"}`); err != nil {
			t.Errorf("write push response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	client := newClient(t, server.URL+"/gateway/", time.Second)
	for range 2 {
		if err := client.RequestPosition(t.Context(), driverID, tripID); err != nil {
			t.Fatalf("RequestPosition() error = %v", err)
		}
	}
	if len(requestIDs) != 2 {
		t.Fatalf("got %d requests, want 2", len(requestIDs))
	}
	if first, second := <-requestIDs, <-requestIDs; first == second {
		t.Errorf("request_id repeated: %q", first)
	}
}

func TestRequestPositionReturnsHTTPStatusWithoutAnotherAttempt(t *testing.T) {
	for _, code := range []int{
		http.StatusBadRequest, http.StatusTooManyRequests, http.StatusServiceUnavailable,
		http.StatusTemporaryRedirect, http.StatusOK,
	} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.Header().Set("Location", "/redirected")
				w.WriteHeader(code)
			}))
			t.Cleanup(server.Close)
			client := newClient(t, server.URL, time.Second)

			err := client.RequestPosition(t.Context(), uuid.New(), uuid.New())
			var status *push.HTTPError
			if !errors.As(err, &status) || status.StatusCode != code {
				t.Errorf("RequestPosition() error = %v, want HTTPError with status %d", err, code)
			}
			if got := requests.Load(); got != 1 {
				t.Errorf("sent %d requests, want exactly 1", got)
			}
		})
	}
}

func TestRequestPositionPreservesTransportError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))
	t.Cleanup(server.Close)
	client := newClient(t, server.URL, time.Second)

	err := client.RequestPosition(t.Context(), uuid.New(), uuid.New())
	var transport *url.Error
	var status *push.HTTPError
	if !errors.As(err, &transport) || errors.As(err, &status) {
		t.Errorf("RequestPosition() error = %T %v, want a transport error", err, err)
	}
}

func TestRequestPositionPropagatesCancellation(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Errorf("read push request: %v", err)
		}
		close(started)
		select {
		case <-r.Context().Done():
		case <-t.Context().Done():
		}
	}))
	t.Cleanup(server.Close)
	client := newClient(t, server.URL, 10*time.Second)
	watch, stop := context.WithTimeout(t.Context(), 5*time.Second)
	defer stop()
	ctx, cancel := context.WithCancel(watch)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- client.RequestPosition(ctx, uuid.New(), uuid.New())
	}()
	select {
	case <-started:
	case <-watch.Done():
		t.Fatal("push request did not reach the server")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("RequestPosition() error = %v, want context cancellation", err)
		}
	case <-watch.Done():
		t.Fatal("push request did not return after cancellation")
	}
}

func TestRequestPositionEnforcesTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-t.Context().Done():
		}
	}))
	t.Cleanup(server.Close)
	client := newClient(t, server.URL, 100*time.Millisecond)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	err := client.RequestPosition(ctx, uuid.New(), uuid.New())
	if !errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
		t.Errorf("RequestPosition() error = %v, context = %v, want the client timeout", err, ctx.Err())
	}
}

func TestRequestPositionClosesRejectedResponse(t *testing.T) {
	closed := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Errorf("read push request: %v", err)
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("flush push response: %v", err)
			return
		}
		select {
		case <-r.Context().Done():
			close(closed)
		case <-t.Context().Done():
		}
	}))
	t.Cleanup(server.Close)
	client := newClient(t, server.URL, 10*time.Second)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	err := client.RequestPosition(ctx, uuid.New(), uuid.New())
	var status *push.HTTPError
	if !errors.As(err, &status) || status.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("RequestPosition() error = %v, want HTTP 503", err)
	}
	select {
	case <-closed:
	case <-ctx.Done():
		t.Fatal("client left the response open")
	}
}

func TestRequestPositionExportsClientSpan(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	previous := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		otel.SetTextMapPropagator(previous)
		_ = provider.Shutdown(context.Background())
	})
	header := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header <- r.Header.Get("traceparent")
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(server.Close)
	client := newClient(t, server.URL, time.Second)
	ctx, parent := provider.Tracer("test").Start(t.Context(), "worker")
	defer parent.End()
	if err := client.RequestPosition(ctx, uuid.New(), uuid.New()); err != nil {
		t.Fatal(err)
	}
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("completed client spans = %d, want one", len(spans))
	}
	span := spans[0]
	if span.Name != "push.request_position" || span.SpanKind != trace.SpanKindClient || span.Parent.SpanID() != parent.SpanContext().SpanID() {
		t.Errorf("unexpected client span: %s, %v, parent %v", span.Name, span.SpanKind, span.Parent)
	}
	want := span.SpanContext.TraceID().String() + "-" + span.SpanContext.SpanID().String()
	if got := <-header; !strings.Contains(got, want) {
		t.Errorf("traceparent = %q, want it to contain %q", got, want)
	}
}

func newClient(t *testing.T, endpoint string, timeout time.Duration) *push.Client {
	t.Helper()
	baseURL, err := url.Parse(endpoint)
	if err != nil {
		t.Fatalf("parse test URL: %v", err)
	}
	client, err := push.New(baseURL, timeout)
	if err != nil {
		t.Fatalf("new push client: %v", err)
	}
	t.Cleanup(client.CloseIdleConnections)
	return client
}
