package create

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
)

const (
	userID   = "11111111-1111-1111-1111-111111111111"
	driverID = "22222222-2222-2222-2222-222222222222"
	zeroID   = "00000000-0000-0000-0000-000000000000"
	okPoint  = `{"latitude": 59.9, "longitude": 30.3}`
)

func body(user, price, start, end string) string {
	return fmt.Sprintf(
		`{"user_id": %q, "driver_id": %q, "price": %s, "start_point": %s, "end_point": %s}`,
		user, driverID, price, start, end,
	)
}

func TestDecode(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{name: "valid body", body: body(userID, "1450", okPoint, okPoint)},
		{name: "zero price is allowed", body: body(userID, "0", okPoint, okPoint)},
		{name: "not json", body: `not json`, wantErr: "body must be a JSON object"},
		{name: "empty object", body: `{}`, wantErr: "user_id is required"},
		{
			name:    "null price",
			body:    body(userID, "null", okPoint, okPoint),
			wantErr: "price is required",
		},
		{
			name:    "start point is not an object",
			body:    body(userID, "100", "5", okPoint),
			wantErr: "start_point must be an object",
		},
		{
			name:    "missing longitude",
			body:    body(userID, "100", `{"latitude": 59.9}`, okPoint),
			wantErr: "start_point.longitude is required",
		},
		{
			name:    "unknown field",
			body:    strings.Replace(body(userID, "100", okPoint, okPoint), `{"user_id"`, `{"extra": 1, "user_id"`, 1),
			wantErr: "invalid body",
		},
		{
			name:    "zero user id",
			body:    body(zeroID, "100", okPoint, okPoint),
			wantErr: "must be non-empty UUIDs",
		},
		{
			name:    "negative price",
			body:    body(userID, "-1", okPoint, okPoint),
			wantErr: "price must be >= 0",
		},
		{
			name:    "latitude out of range",
			body:    body(userID, "100", `{"latitude": 91, "longitude": 30.3}`, okPoint),
			wantErr: "start_point.latitude must be between -90 and 90",
		},
		{
			name:    "longitude out of range",
			body:    body(userID, "100", okPoint, `{"latitude": 59.9, "longitude": 181}`),
			wantErr: "end_point.longitude must be between -180 and 180",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/trips", strings.NewReader(tt.body))

			got, err := decode(w, r)

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("decode() unexpected error: %v", err)
				}
				if got.Price < 0 {
					t.Errorf("price = %d, want >= 0", got.Price)
				}
				return
			}

			var validation *trip.ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("decode() error = %v, want *trip.ValidationError", err)
			}
			if !strings.Contains(validation.Message, tt.wantErr) {
				t.Errorf("message = %q, want it to contain %q", validation.Message, tt.wantErr)
			}
		})
	}
}
