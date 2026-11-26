package telemetry

import (
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
)

// ErrorHandler пишет ошибки самого OTel SDK в наш лог, но не чаще раза в every.
// Когда коллектор недоступен, SDK сообщает о каждом неудавшемся экспорте
// (трейсы раз в несколько секунд, метрики раз в 10 с), и без троттлинга лог
// превращается в поток одинаковых строк. Число подавленных ошибок
// добавляется к следующей записи.
type ErrorHandler struct {
	log   *slog.Logger
	every time.Duration
	now   func() time.Time

	mu         sync.Mutex
	last       time.Time
	suppressed int
}

var _ otel.ErrorHandler = (*ErrorHandler)(nil)

func NewErrorHandler(log *slog.Logger, every time.Duration) *ErrorHandler {
	return &ErrorHandler{log: log, every: every, now: time.Now}
}

// Handle пишет Warn, а не Error: сервис без телеметрии продолжает работать.
func (h *ErrorHandler) Handle(err error) {
	h.mu.Lock()
	now := h.now()
	if !h.last.IsZero() && now.Sub(h.last) < h.every {
		h.suppressed++
		h.mu.Unlock()
		return
	}
	suppressed := h.suppressed
	h.last, h.suppressed = now, 0
	h.mu.Unlock()

	attrs := []any{slog.Any("error", err)}
	if suppressed > 0 {
		attrs = append(attrs, slog.Int("suppressed", suppressed))
	}
	h.log.Warn("telemetry error", attrs...)
}
