package middleware

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type routeKey struct{}

type routeHolder struct {
	pattern string
}

func Tracing(next http.Handler) http.Handler {
	return otelhttp.NewHandler(withRoute(next), "http.server",
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

func Route(ctx context.Context) string {
	holder, _ := ctx.Value(routeKey{}).(*routeHolder)
	if holder == nil {
		return ""
	}
	return holder.pattern
}

func TagRoute(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rctx := chi.RouteContext(r.Context()); rctx != nil {
			pattern := rctx.RoutePattern()

			if holder, ok := r.Context().Value(routeKey{}).(*routeHolder); ok {
				holder.pattern = pattern
			}

			span := trace.SpanFromContext(r.Context())
			span.SetName(r.Method + " " + pattern)
			span.SetAttributes(attribute.String("http.route", pattern))
		}
		next.ServeHTTP(w, r)
	})
}
