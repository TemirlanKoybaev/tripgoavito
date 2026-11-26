package middleware

import (
	"errors"
	"log/slog"
	"net/http"

	"go.opentelemetry.io/otel/trace"

	"github.com/TemirlanKoybaev/tripgoavito.git/internal/http/domainerror"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/http/response"
)

// observedResponse передаёт результат синхронного вызова хендлера в Logging и Tracing.
type observedResponse struct {
	statusRecorder
	logging bool
	errType string
	cause   error
}

func observeResponse(w http.ResponseWriter) *observedResponse {
	if rec, ok := w.(*observedResponse); ok {
		return rec
	}
	return &observedResponse{statusRecorder: statusRecorder{ResponseWriter: w, status: http.StatusOK}}
}

// RespondError обрабатывает результат хендлера на границе с net/http.
// После отправки статуса остаётся только сообщить об ошибке в лог и спан.
func RespondError(logger *slog.Logger, w http.ResponseWriter, r *http.Request, err error) {
	if err == nil {
		return
	}
	if written, ok := errors.AsType[*response.WrittenError](err); ok {
		reportError(logger, w, r, written.Status, written.Code, written.Cause)
		return
	}

	status, problem := domainerror.Problem(err)
	writeErr := domainerror.Write(w, r, problem)
	switch {
	case status < http.StatusInternalServerError:
		err = writeErr
	case writeErr != nil:
		err = errors.Join(err, writeErr)
	}
	reportError(logger, w, r, status, problem.Code, err)
}

func reportError(logger *slog.Logger, w http.ResponseWriter, r *http.Request, status int, code string, cause error) {
	ctx := r.Context()
	rec := findObservedResponse(w)
	if rec == nil {
		markSpan(trace.SpanFromContext(ctx), status, code, cause, false)
	} else {
		rec.errType = code
		rec.cause = cause
		if rec.logging {
			return
		}
	}
	if cause != nil {
		level := slog.LevelWarn
		if status >= http.StatusInternalServerError {
			level = slog.LevelError
		}
		logger.LogAttrs(ctx, level, "response error",
			slog.String("method", r.Method), slog.Int("status", status), slog.String("error", cause.Error()))
	}
}

func findObservedResponse(w http.ResponseWriter) *observedResponse {
	for {
		if rec, ok := w.(*observedResponse); ok {
			return rec
		}
		wrapper, ok := w.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return nil
		}
		w = wrapper.Unwrap()
	}
}
