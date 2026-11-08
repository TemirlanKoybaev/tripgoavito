package create_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/usecase/trip/create"
)

// commitFunc выполняет fn и возвращает commitErr как ошибку самого commit.
func commitFunc(commitErr error) func(context.Context, func(context.Context) error) error {
	return func(ctx context.Context, fn func(context.Context) error) error {
		if err := fn(ctx); err != nil {
			return err
		}
		return commitErr
	}
}

func TestUseCaseCreate(t *testing.T) {
	in := create.Input{
		UserID:         uuid.New(),
		DriverID:       uuid.New(),
		StartLatitude:  59.9398,
		StartLongitude: 30.3146,
		EndLatitude:    59.929,
		EndLongitude:   30.3626,
		Price:          1450,
	}
	errDB := errors.New("db is down")

	tests := []struct {
		name      string
		setup     func(trips *MocktripRepository, history *MockhistoryRepository)
		commitErr error
		wantErr   error
	}{
		{
			name: "trip is created as active and history is written",
			setup: func(trips *MocktripRepository, history *MockhistoryRepository) {
				trips.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
				history.EXPECT().Create(gomock.Any(), gomock.Any(), gomock.Nil(), trip.StatusActive).Return(nil)
			},
		},
		{
			name: "busy driver returns error and history is not written",
			setup: func(trips *MocktripRepository, _ *MockhistoryRepository) {
				trips.EXPECT().Create(gomock.Any(), gomock.Any()).Return(trip.ErrDriverBusy)
			},
			wantErr: trip.ErrDriverBusy,
		},
		{
			name: "history failure is returned",
			setup: func(trips *MocktripRepository, history *MockhistoryRepository) {
				trips.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
				history.EXPECT().Create(gomock.Any(), gomock.Any(), gomock.Nil(), trip.StatusActive).Return(errDB)
			},
			wantErr: errDB,
		},
		{
			name: "commit failure is returned and trip is not counted",
			setup: func(trips *MocktripRepository, history *MockhistoryRepository) {
				trips.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
				history.EXPECT().Create(gomock.Any(), gomock.Any(), gomock.Nil(), trip.StatusActive).Return(nil)
			},
			commitErr: errDB,
			wantErr:   errDB,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			ctrl := gomock.NewController(t)
			trips := NewMocktripRepository(ctrl)
			history := NewMockhistoryRepository(ctrl)
			txManager := NewMocktxManager(ctrl)
			metrics := NewMocktripMetrics(ctrl)

			txManager.EXPECT().Do(gomock.Any(), gomock.Any()).DoAndReturn(commitFunc(tt.commitErr))
			tt.setup(trips, history)
			// Счётчик ожидается только на успехе; на ошибке gomock упадёт от лишнего вызова.
			if tt.wantErr == nil {
				metrics.EXPECT().TripCreated(gomock.Any())
			}

			got, err := create.New(trips, history, txManager, metrics, logger).Create(context.Background(), in)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Create() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				if got != nil {
					t.Errorf("Create() = %v, want nil on error", got)
				}
				return
			}
			if got.ID == uuid.Nil || got.Status != trip.StatusActive || got.FinishedAt != nil {
				t.Errorf("trip = %+v, want new active trip", got)
			}
			if got.UserID != in.UserID || got.DriverID != in.DriverID || got.Price != in.Price {
				t.Errorf("trip = %+v does not match input %+v", got, in)
			}
		})
	}
}
