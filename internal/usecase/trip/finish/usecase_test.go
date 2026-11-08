package finish_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/usecase/trip/finish"
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

func TestUseCaseFinish(t *testing.T) {
	id := uuid.New()
	errDB := errors.New("db is down")

	tests := []struct {
		name      string
		setup     func(trips *MocktripRepository, history *MockhistoryRepository)
		commitErr error
		wantErr   error
	}{
		{
			name: "active trip is finished and history is written",
			setup: func(trips *MocktripRepository, history *MockhistoryRepository) {
				trips.EXPECT().GetForUpdate(gomock.Any(), id).
					Return(&trip.Trip{ID: id, Status: trip.StatusActive}, nil)
				trips.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
				history.EXPECT().Create(gomock.Any(), id, gomock.Any(), trip.StatusCompleted).Return(nil)
			},
		},
		{
			name: "completed trip is rejected and nothing is written",
			setup: func(trips *MocktripRepository, _ *MockhistoryRepository) {
				trips.EXPECT().GetForUpdate(gomock.Any(), id).
					Return(&trip.Trip{ID: id, Status: trip.StatusCompleted}, nil)
			},
			wantErr: trip.ErrTripCompleted,
		},
		{
			name: "missing trip returns not found",
			setup: func(trips *MocktripRepository, _ *MockhistoryRepository) {
				trips.EXPECT().GetForUpdate(gomock.Any(), id).Return(nil, trip.ErrTripNotFound)
			},
			wantErr: trip.ErrTripNotFound,
		},
		{
			name: "history failure is returned",
			setup: func(trips *MocktripRepository, history *MockhistoryRepository) {
				trips.EXPECT().GetForUpdate(gomock.Any(), id).
					Return(&trip.Trip{ID: id, Status: trip.StatusActive}, nil)
				trips.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
				history.EXPECT().Create(gomock.Any(), id, gomock.Any(), trip.StatusCompleted).Return(errDB)
			},
			wantErr: errDB,
		},
		{
			name: "commit failure is returned and trip is not counted",
			setup: func(trips *MocktripRepository, history *MockhistoryRepository) {
				trips.EXPECT().GetForUpdate(gomock.Any(), id).
					Return(&trip.Trip{ID: id, Status: trip.StatusActive}, nil)
				trips.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
				history.EXPECT().Create(gomock.Any(), id, gomock.Any(), trip.StatusCompleted).Return(nil)
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
				metrics.EXPECT().TripCompleted(gomock.Any())
			}

			got, err := finish.New(trips, history, txManager, metrics, logger).Finish(context.Background(), id)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Finish() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				if got != nil {
					t.Errorf("Finish() = %v, want nil on error", got)
				}
				return
			}
			if got.Status != trip.StatusCompleted || got.FinishedAt == nil {
				t.Errorf("trip = %+v, want completed with FinishedAt set", got)
			}
		})
	}
}
