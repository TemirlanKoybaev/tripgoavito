package middleware

import (
	"context"
	"net/http"
	"time"
)

type httpMetrics interface {
	AddActiveRequest(ctx context.Context, delta int64, method, route string)
	RecordRequest(ctx context.Context, d time.Duration, method, route string, status int)
}

// Metrics пишет длительность и число активных запросов. Должен стоять внутри
// Tracing: шаблон маршрута он получает через routeHolder, когда chi найдёт маршрут.
func Metrics(m httpMetrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			ctx := r.Context()
			panicked := true

			// Маршрут неизвестен до работы роутера, поэтому +1 ставим в onMatch,
			// а -1 в defer с теми же атрибутами. Не найденный маршрут в active не считаем.
			var matched string
			holder := holderFrom(ctx)
			if holder != nil {
				holder.onMatch = func(pattern string) {
					matched = pattern
					m.AddActiveRequest(ctx, 1, r.Method, pattern)
				}
			}

			defer func() {
				status := rec.status
				if panicked {
					status = http.StatusInternalServerError
				}
				if matched != "" {
					m.AddActiveRequest(ctx, -1, r.Method, matched)
				}
				m.RecordRequest(ctx, time.Since(start), r.Method, Route(ctx), status)
			}()

			next.ServeHTTP(rec, r)
			panicked = false
		})
	}
}
