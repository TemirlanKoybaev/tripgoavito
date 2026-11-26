package telemetry

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestErrorHandlerThrottles(t *testing.T) {
	var buf bytes.Buffer
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	h := NewErrorHandler(slog.New(slog.NewTextHandler(&buf, nil)), 30*time.Second)
	h.now = func() time.Time { return now }
	err := errors.New("export failed")

	h.Handle(err) // первая ошибка пишется сразу
	now = now.Add(5 * time.Second)
	h.Handle(err) // внутри окна подавляются
	now = now.Add(10 * time.Second)
	h.Handle(err)

	if got := strings.Count(buf.String(), "telemetry error"); got != 1 {
		t.Fatalf("records inside window = %d, want 1\n%s", got, buf.String())
	}

	now = now.Add(30 * time.Second)
	h.Handle(err) // окно прошло: запись с числом подавленных

	out := buf.String()
	if got := strings.Count(out, "telemetry error"); got != 2 {
		t.Fatalf("records after window = %d, want 2\n%s", got, out)
	}
	if !strings.Contains(out, "suppressed=2") {
		t.Errorf("second record must report 2 suppressed errors\n%s", out)
	}
}
