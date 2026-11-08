package domainerror_test

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/http/domainerror"
)

func TestProblem(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
		wantType   string
		wantDetail string
	}{
		{
			name:       "trip not found",
			err:        trip.ErrTripNotFound,
			wantStatus: http.StatusNotFound,
			wantCode:   "trip_not_found",
			wantType:   "https://tripgo.example/problems/trip-not-found",
			wantDetail: "Trip not found",
		},
		{
			name:       "trip completed",
			err:        trip.ErrTripCompleted,
			wantStatus: http.StatusConflict,
			wantCode:   "trip_completed",
			wantType:   "https://tripgo.example/problems/trip-completed",
			wantDetail: "Trip already completed",
		},
		{
			name:       "driver busy",
			err:        trip.ErrDriverBusy,
			wantStatus: http.StatusConflict,
			wantCode:   "driver_busy",
			wantType:   "https://tripgo.example/problems/driver-busy",
			wantDetail: "Driver already has an active trip",
		},
		{
			name:       "wrapped sentinel error",
			err:        fmt.Errorf("finish trip: %w", trip.ErrTripNotFound),
			wantStatus: http.StatusNotFound,
			wantCode:   "trip_not_found",
			wantType:   "https://tripgo.example/problems/trip-not-found",
			wantDetail: "Trip not found",
		},
		{
			name:       "validation error keeps its message",
			err:        &trip.ValidationError{Message: "price must be >= 0"},
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_request",
			wantType:   "https://tripgo.example/problems/invalid-request",
			wantDetail: "price must be >= 0",
		},
		{
			name:       "wrapped validation error",
			err:        fmt.Errorf("decode: %w", &trip.ValidationError{Message: "bad body"}),
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_request",
			wantType:   "https://tripgo.example/problems/invalid-request",
			wantDetail: "bad body",
		},
		{
			name:       "unknown error does not leak details",
			err:        errors.New("pq: password authentication failed for user tripgo"),
			wantStatus: http.StatusInternalServerError,
			wantCode:   "internal_error",
			wantType:   "https://tripgo.example/problems/internal-error",
			wantDetail: "Internal server error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, p := domainerror.Problem(tt.err)

			if status != tt.wantStatus {
				t.Errorf("status = %d, want %d", status, tt.wantStatus)
			}
			if int(p.Status) != tt.wantStatus {
				t.Errorf("problem.Status = %d, want %d", p.Status, tt.wantStatus)
			}
			if p.Code != tt.wantCode {
				t.Errorf("code = %q, want %q", p.Code, tt.wantCode)
			}
			if p.Type != tt.wantType {
				t.Errorf("type = %q, want %q", p.Type, tt.wantType)
			}
			if p.Detail == nil || *p.Detail != tt.wantDetail {
				t.Errorf("detail = %v, want %q", p.Detail, tt.wantDetail)
			}
		})
	}
}
