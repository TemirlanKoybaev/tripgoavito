package create_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/TemirlanKoybaev/tripgoavito.git/api"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/http/trip/create"
	createuc "github.com/TemirlanKoybaev/tripgoavito.git/internal/usecase/trip/create"
)

const (
	problemContentType = "application/problem+json"
	validBody          = `{
		"user_id": "11111111-1111-1111-1111-111111111111",
		"driver_id": "22222222-2222-2222-2222-222222222222",
		"price": 1450,
		"start_point": {"latitude": 59.9398, "longitude": 30.3146},
		"end_point": {"latitude": 59.929, "longitude": 30.3626}
	}`
)

func TestHandlerCreate(t *testing.T) {
	wantInput := createuc.Input{
		UserID:         uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		DriverID:       uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		StartLatitude:  59.9398,
		StartLongitude: 30.3146,
		EndLatitude:    59.929,
		EndLongitude:   30.3626,
		Price:          1450,
	}
	created := &trip.Trip{ID: uuid.New(), UserID: wantInput.UserID, DriverID: wantInput.DriverID, Status: trip.StatusActive}
	internal := errors.New("pq: password authentication failed for user tripgo")

	tests := []struct {
		name       string
		body       string
		callsUC    bool
		trip       *trip.Trip
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "success", body: validBody, callsUC: true, trip: created, wantStatus: http.StatusCreated},
		{
			name:       "invalid body never reaches usecase",
			body:       `{"price": -1}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_request",
		},
		{
			name:       "driver is busy",
			body:       validBody,
			callsUC:    true,
			err:        trip.ErrDriverBusy,
			wantStatus: http.StatusConflict,
			wantCode:   "driver_busy",
		},
		{
			name:       "unknown error is hidden from the client",
			body:       validBody,
			callsUC:    true,
			err:        internal,
			wantStatus: http.StatusInternalServerError,
			wantCode:   "internal_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			uc := NewMockusecase(ctrl)
			if tt.callsUC {
				uc.EXPECT().Create(gomock.Any(), wantInput).Return(tt.trip, tt.err)
			}

			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/trips", strings.NewReader(tt.body))

			create.New(uc).Handle(w, r, api.CreateTripParams{})

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, tt.wantStatus)
			}

			if tt.wantCode == "" {
				if loc := w.Header().Get("Location"); loc != "/api/v1/trips/"+created.ID.String() {
					t.Errorf("location = %q, want link to the created trip", loc)
				}
				var got api.Trip
				if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
					t.Fatalf("decode body: %v", err)
				}
				if got.Id != created.ID || got.Status != api.TripStatus(trip.StatusActive) {
					t.Errorf("trip = %+v, want active trip %s", got, created.ID)
				}
				return
			}

			if ct := w.Header().Get("Content-Type"); ct != problemContentType {
				t.Errorf("content-type = %q, want %q", ct, problemContentType)
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
