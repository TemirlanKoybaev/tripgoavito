package trip_test

import (
	"errors"
	"testing"
	"time"

	"github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
)

func TestTripFinish(t *testing.T) {
	finishedAt := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name           string
		status         string
		wantErr        error
		wantStatus     string
		wantFinishedAt bool
	}{
		{
			name:           "active trip is finished",
			status:         trip.StatusActive,
			wantErr:        nil,
			wantStatus:     trip.StatusCompleted,
			wantFinishedAt: true,
		},
		{
			name:           "completed trip is rejected and unchanged",
			status:         trip.StatusCompleted,
			wantErr:        trip.ErrTripCompleted,
			wantStatus:     trip.StatusCompleted,
			wantFinishedAt: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := &trip.Trip{Status: tt.status}

			err := tr.Finish(finishedAt)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Finish() error = %v, want %v", err, tt.wantErr)
			}
			if tr.Status != tt.wantStatus {
				t.Errorf("status = %q, want %q", tr.Status, tt.wantStatus)
			}
			if tt.wantFinishedAt {
				if tr.FinishedAt == nil || !tr.FinishedAt.Equal(finishedAt) {
					t.Errorf("FinishedAt = %v, want %v", tr.FinishedAt, finishedAt)
				}
			} else if tr.FinishedAt != nil {
				t.Errorf("FinishedAt = %v, want nil", tr.FinishedAt)
			}
		})
	}
}
