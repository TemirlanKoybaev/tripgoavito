package domainerror_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/http/domainerror"
)

func TestWriteRecordsInternalCauseOnly(t *testing.T) {
	errDB := errors.New("pq: password authentication failed")

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantStored error
	}{
		{"internal error is stored for the request log", errDB, http.StatusInternalServerError, errDB},
		{"client error is not stored", trip.ErrTripNotFound, http.StatusNotFound, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, slot := domainerror.WithSlot(context.Background())
			r := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/trips/1", http.NoBody)
			w := httptest.NewRecorder()

			domainerror.Write(w, r, tt.err)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
			if got := slot.Err(); !errors.Is(got, tt.wantStored) {
				t.Errorf("slot error = %v, want %v", got, tt.wantStored)
			}
			if strings.Contains(w.Body.String(), "password") {
				t.Errorf("response leaks internal cause: %s", w.Body.String())
			}
		})
	}
}

func TestRecordKeepsFirstErrorAndIgnoresMissingSlot(t *testing.T) {
	first, second := errors.New("first"), errors.New("second")

	// Без слота в контексте Record не должен паниковать.
	domainerror.Record(context.Background(), first)

	ctx, slot := domainerror.WithSlot(context.Background())
	domainerror.Record(ctx, first)
	domainerror.Record(ctx, second)

	if got := slot.Err(); !errors.Is(got, first) {
		t.Errorf("slot error = %v, want first", got)
	}
}
