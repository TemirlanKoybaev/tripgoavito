package middleware

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
)

type routeKey struct{}

// routeHolder передаёт шаблон маршрута из роутера во внешние middleware:
// chi находит маршрут уже после них, а шаблон нужен им после ответа.
type routeHolder struct {
	pattern string
	onMatch func(pattern string)
}

func (h *routeHolder) match(pattern string) {
	h.pattern = pattern
	if h.onMatch != nil {
		h.onMatch(pattern)
	}
}

// Tracing создаёт корневой спан запроса. Встроенные метрики otelhttp отключены:
// в них http.route был бы пустым, а нам нужен шаблон (см. Metrics).
func Tracing(next http.Handler) http.Handler {
	return otelhttp.NewHandler(withRoute(next), "http.server",
		// http.route в метриках должен быть шаблоном, а otelhttp его не знает,
		// поэтому метрики пишет наш middleware Metrics.
		otelhttp.WithMeterProvider(noop.NewMeterProvider()),
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return r.Method
		}),
	)
}

func withRoute(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), routeKey{}, &routeHolder{})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func holderFrom(ctx context.Context) *routeHolder {
	holder, _ := ctx.Value(routeKey{}).(*routeHolder)
	return holder
}

func Route(ctx context.Context) string {
	if holder := holderFrom(ctx); holder != nil {
		return holder.pattern
	}
	return ""
}

// TagRoute вызывается chi, когда маршрут найден: проставляет шаблон в спан
// и уведомляет подписчиков holder.
func TagRoute(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rctx := chi.RouteContext(r.Context()); rctx != nil {
			pattern := rctx.RoutePattern()

			if holder := holderFrom(r.Context()); holder != nil {
				holder.match(pattern)
			}

			span := trace.SpanFromContext(r.Context())
			span.SetName(r.Method + " " + pattern)
			span.SetAttributes(attribute.String("http.route", pattern))
		}
		next.ServeHTTP(w, r)
	})
}
