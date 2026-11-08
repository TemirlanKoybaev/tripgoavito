package get_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/usecase/trip/get"
)

func TestUseCaseGet(t *testing.T) {
	id := uuid.New()
	found := &trip.Trip{ID: id, Status: trip.StatusActive}
	errDB := errors.New("db is down")

	tests := []struct {
		name     string
		repoTrip *trip.Trip
		repoErr  error
		wantErr  error
	}{
		{name: "existing trip is returned", repoTrip: found},
		{name: "missing trip returns not found", repoErr: trip.ErrTripNotFound, wantErr: trip.ErrTripNotFound},
		{name: "repository failure is returned", repoErr: errDB, wantErr: errDB},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			trips := NewMocktripRepository(ctrl)
			trips.EXPECT().GetByID(gomock.Any(), id).Return(tt.repoTrip, tt.repoErr)

			got, err := get.New(trips).Get(context.Background(), id)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Get() error = %v, want %v", err, tt.wantErr)
			}
			if got != tt.repoTrip {
				t.Errorf("Get() = %v, want %v", got, tt.repoTrip)
			}
		})
	}
}
