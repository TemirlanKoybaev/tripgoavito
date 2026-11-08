package middleware

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"

	"github.com/TemirlanKoybaev/tripgoavito.git/internal/http/domainerror"
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
	return otelhttp.NewHandler(withRequestState(next), "http.server",
		// http.route в метриках должен быть шаблоном, а otelhttp его не знает,
		// поэтому метрики пишет наш middleware Metrics.
		otelhttp.WithMeterProvider(noop.NewMeterProvider()),
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return r.Method
		}),
	)
}

// withRequestState кладёт в контекст состояние запроса (шаблон маршрута и слот
// ошибки) и после ответа размечает корневой спан по итогу запроса.
func withRequestState(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), routeKey{}, &routeHolder{})
		ctx, slot := domainerror.WithSlot(ctx)
		r = r.WithContext(ctx)

		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		panicked := true
		defer func() {
			status := rec.status
			if panicked {
				status = http.StatusInternalServerError
			}
			markSpan(trace.SpanFromContext(ctx), status, slot, panicked)
		}()

		next.ServeHTTP(rec, r)
		panicked = false
	})
}

// markSpan ставит error.type на любую ошибку, а статус Error только на 5xx:
// 4xx — ошибка клиента, а не сервиса, спан у неё остаётся без ошибки.
// error.type берём из кода доменной ошибки (trip_not_found), а если кода нет,
// из статуса ("404"): оба значения низкокардинальны, в отличие от текста ошибки.
func markSpan(span trace.Span, status int, slot *domainerror.Slot, panicked bool) {
	errType := slot.Type()
	if errType == "" {
		switch {
		case panicked:
			errType = "panic"
		case status >= http.StatusBadRequest:
			errType = strconv.Itoa(status)
		}
	}
	if errType != "" {
		span.SetAttributes(attribute.String("error.type", errType))
	}

	if status >= http.StatusInternalServerError {
		if err := slot.Err(); err != nil {
			span.RecordError(err)
		}
		// Описание не задаём: otelhttp после хендлера всё равно перезапишет его пустым,
		// а тип ошибки уже лежит в error.type.
		span.SetStatus(codes.Error, "")
	}
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
