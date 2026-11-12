package middleware_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/http/domainerror"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/http/middleware"
)

const codeInternalError = "internal_error"

// logRecord — одна JSON-строка лога.
type logRecord map[string]any

func newTestLogger() (*slog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	return slog.New(slog.NewJSONHandler(buf, nil)), buf
}

func parseLog(t *testing.T, buf *bytes.Buffer) []logRecord {
	t.Helper()

	var records []logRecord
	dec := json.NewDecoder(buf)
	for dec.More() {
		var rec logRecord
		if err := dec.Decode(&rec); err != nil {
			t.Fatalf("decode log: %v", err)
		}
		records = append(records, rec)
	}
	return records
}

func findLog(records []logRecord, msg string) logRecord {
	for _, rec := range records {
		if rec["msg"] == msg {
			return rec
		}
	}
	return nil
}

func TestRecover(t *testing.T) {
	logger, buf := newTestLogger()
	panicking := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") })

	w := httptest.NewRecorder()
	middleware.Recover(logger)(panicking).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/trips/1", http.NoBody))

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("content-type = %q, want application/problem+json", ct)
	}

	var problem struct {
		Code   string `json:"code"`
		Status int    `json:"status"`
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &problem); err != nil {
		t.Fatalf("body is not problem json: %v\n%s", err, w.Body.String())
	}
	if problem.Code != codeInternalError || problem.Status != http.StatusInternalServerError {
		t.Errorf("problem = %+v, want internal_error/500", problem)
	}
	if problem.Detail != "Internal server error" {
		t.Errorf("detail = %q, panic text must not leak to the client", problem.Detail)
	}

	rec := findLog(parseLog(t, buf), "panic recovered")
	if rec == nil {
		t.Fatalf("no \"panic recovered\" record in log:\n%s", buf.String())
	}
	if rec["level"] != "ERROR" {
		t.Errorf("level = %v, want ERROR", rec["level"])
	}
	if rec["panic"] != "boom" {
		t.Errorf("panic = %v, want boom", rec["panic"])
	}
	if stack, _ := rec["stack"].(string); stack == "" {
		t.Error("record must carry the stack trace")
	}
}

func TestLogging(t *testing.T) {
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantStatus int
		wantLevel  string
		wantError  string
	}{
		{
			name:       "2xx is info",
			handler:    func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusCreated) },
			wantStatus: http.StatusCreated,
			wantLevel:  "INFO",
		},
		{
			name: "4xx is info, not error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				domainerror.Write(w, r, trip.ErrTripNotFound)
			},
			wantStatus: http.StatusNotFound,
			wantLevel:  "INFO",
		},
		{
			name: "5xx is error and carries the real cause",
			handler: func(w http.ResponseWriter, r *http.Request) {
				domainerror.Write(w, r, errors.New("db is down"))
			},
			wantStatus: http.StatusInternalServerError,
			wantLevel:  "ERROR",
			wantError:  "db is down",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, buf := newTestLogger()

			w := httptest.NewRecorder()
			middleware.Logging(logger)(tt.handler).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/trips", http.NoBody))

			records := parseLog(t, buf)
			if len(records) != 1 {
				t.Fatalf("records = %d, want 1:\n%s", len(records), buf.String())
			}
			rec := records[0]

			if rec["msg"] != "request handled" {
				t.Errorf("msg = %v, want request handled", rec["msg"])
			}
			if rec["level"] != tt.wantLevel {
				t.Errorf("level = %v, want %s", rec["level"], tt.wantLevel)
			}
			if rec["status"] != float64(tt.wantStatus) {
				t.Errorf("status = %v, want %d", rec["status"], tt.wantStatus)
			}
			if rec["method"] != http.MethodPost {
				t.Errorf("method = %v, want POST", rec["method"])
			}

			gotErr, hasErr := rec["error"].(string)
			if tt.wantError == "" && hasErr {
				t.Errorf("unexpected error attr %q on a non-5xx record", gotErr)
			}
			if tt.wantError != "" && gotErr != tt.wantError {
				t.Errorf("error = %q, want %q", gotErr, tt.wantError)
			}
		})
	}
}
