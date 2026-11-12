package finish_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/TemirlanKoybaev/tripgoavito.git/api"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/http/trip/finish"
)

const problemContentType = "application/problem+json"

func TestHandlerFinish(t *testing.T) {
	id := uuid.New()
	finishedAt := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	completed := &trip.Trip{
		ID:         id,
		Status:     trip.StatusCompleted,
		StartedAt:  finishedAt.Add(-time.Hour),
		FinishedAt: &finishedAt,
	}
	internal := errors.New("pq: password authentication failed for user tripgo")

	tests := []struct {
		name        string
		trip        *trip.Trip
		err         error
		wantStatus  int
		wantCode    string
		wantContent string
	}{
		{name: "success", trip: completed, wantStatus: http.StatusOK, wantContent: "application/json"},
		{
			name:        "trip not found",
			err:         trip.ErrTripNotFound,
			wantStatus:  http.StatusNotFound,
			wantCode:    "trip_not_found",
			wantContent: problemContentType,
		},
		{
			name:        "trip already completed",
			err:         trip.ErrTripCompleted,
			wantStatus:  http.StatusConflict,
			wantCode:    "trip_completed",
			wantContent: problemContentType,
		},
		{
			name:        "unknown error is hidden from the client",
			err:         internal,
			wantStatus:  http.StatusInternalServerError,
			wantCode:    "internal_error",
			wantContent: problemContentType,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			uc := NewMockusecase(ctrl)
			uc.EXPECT().Finish(gomock.Any(), id).Return(tt.trip, tt.err)

			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/trips/"+id.String()+"/finish", nil)

			finish.New(uc).Handle(w, r, id)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, tt.wantStatus)
			}
			if ct := w.Header().Get("Content-Type"); ct != tt.wantContent {
				t.Errorf("content-type = %q, want %q", ct, tt.wantContent)
			}

			if tt.wantCode == "" {
				var got api.Trip
				if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
					t.Fatalf("decode body: %v", err)
				}
				if got.Id != id || got.Status != api.TripStatus(trip.StatusCompleted) {
					t.Errorf("trip = %+v, want completed trip %s", got, id)
				}
				if got.FinishedAt == nil || !got.FinishedAt.Equal(finishedAt) {
					t.Errorf("finished_at = %v, want %v", got.FinishedAt, finishedAt)
				}
				return
			}

			if strings.Contains(w.Body.String(), "password") {
				t.Errorf("body leaks internal error: %s", w.Body.String())
			}
			var p api.Problem
			if err := json.NewDecoder(w.Body).Decode(&p); err != nil {
				t.Fatalf("decode problem: %v", err)
			}
			if p.Code != tt.wantCode || int(p.Status) != tt.wantStatus {
				t.Errorf("problem = %+v, want code %q status %d", p, tt.wantCode, tt.wantStatus)
			}
		})
	}
}
