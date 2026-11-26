package push

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/metric/noop"
)

const pushPath = "/api/v1/push"

type Client struct {
	endpoint  string
	http      *http.Client
	transport *http.Transport
}

type HTTPError struct {
	StatusCode int
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("push service returned HTTP %d", e.StatusCode)
}

type positionRequest struct {
	RequestID   string            `json:"request_id"`
	RecipientID uuid.UUID         `json:"recipient_id"`
	Kind        string            `json:"kind"`
	Data        map[string]string `json:"data"`
}

func New(baseURL *url.URL, timeout time.Duration) (*Client, error) {
	if baseURL == nil {
		return nil, fmt.Errorf("push service URL is required")
	}
	if timeout <= 0 {
		return nil, fmt.Errorf("push request timeout must be > 0")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	return &Client{
		endpoint:  baseURL.JoinPath(pushPath).String(),
		transport: transport,
		http: &http.Client{
			Transport: otelhttp.NewTransport(transport,
				otelhttp.WithMeterProvider(noop.NewMeterProvider()),
				otelhttp.WithSpanNameFormatter(func(string, *http.Request) string { return "push.request_position" }),
			),
			Timeout: timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func (c *Client) RequestPosition(ctx context.Context, driverID, tripID uuid.UUID) error {
	body, err := json.Marshal(positionRequest{
		RequestID:   uuid.NewString(),
		RecipientID: driverID,
		Kind:        "REQUEST_POSITION",
		Data:        map[string]string{"trip_id": tripID.String()},
	})
	if err != nil {
		return fmt.Errorf("encode push request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build push request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("send push request: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusAccepted {
		return &HTTPError{StatusCode: response.StatusCode}
	}
	// Дочитываем ответ, чтобы транспорт мог переиспользовать соединение.
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		return fmt.Errorf("read push response: %w", err)
	}
	return nil
}

func (c *Client) CloseIdleConnections() {
	c.transport.CloseIdleConnections()
}
