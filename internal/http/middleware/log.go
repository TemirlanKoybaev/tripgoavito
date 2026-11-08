package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/TemirlanKoybaev/tripgoavito.git/internal/http/domainerror"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Unwrap() http.ResponseWriter {
	return s.ResponseWriter
}

func Logging(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ctx, errSlot := domainerror.WithSlot(r.Context())
			r = r.WithContext(ctx)
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			panicked := true

			defer func() {
				status := rec.status
				if panicked {
					status = http.StatusInternalServerError
				}

				// Error только для 5xx. Сбой записи ответа при статусе ниже 500 — Warn.
				level := slog.LevelInfo
				reqErr := errSlot.Err()
				switch {
				case status >= http.StatusInternalServerError:
					level = slog.LevelError
				case reqErr != nil:
					level = slog.LevelWarn
				}

				attrs := []slog.Attr{
					slog.String("method", r.Method),
					slog.String("route", Route(r.Context())),
					slog.Int("status", status),
					slog.Float64("duration_ms", float64(time.Since(start).Microseconds())/1000),
				}
				if reqErr != nil {
					attrs = append(attrs, slog.String("error", reqErr.Error()))
				}
				logger.LogAttrs(r.Context(), level, "request handled", attrs...)
			}()

			next.ServeHTTP(rec, r)
			panicked = false
		})
	}
}

func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if p := recover(); p != nil {
					logger.ErrorContext(r.Context(), "panic recovered",
						slog.Any("panic", p),
						slog.String("stack", string(debug.Stack())),
					)
					domainerror.Write(w, r, fmt.Errorf("panic: %v", p))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
