package finish_test

import (
	"bytes"
	"context"
	"encoding/json"
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
	fromActive := trip.StatusActive

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
				// from должен быть предыдущим статусом (active): ловит регрессию «в историю
				// записали пустой или уже новый статус». gomock.Eq(&x) сравнивает по значению.
				history.EXPECT().Create(gomock.Any(), id, gomock.Eq(&fromActive), trip.StatusCompleted).Return(nil)
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
			// Ловит регрессию «ошибку Update проглотили»: история и счётчик не должны
			// сработать, а вызывающий должен получить ошибку (history/metrics без EXPECT).
			name: "trips.Update failure is returned and history is not written",
			setup: func(trips *MocktripRepository, _ *MockhistoryRepository) {
				trips.EXPECT().GetForUpdate(gomock.Any(), id).
					Return(&trip.Trip{ID: id, Status: trip.StatusActive}, nil)
				trips.EXPECT().Update(gomock.Any(), gomock.Any()).Return(errDB)
			},
			wantErr: errDB,
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

// logRecords разбирает JSON-лог по записям.
func logRecords(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()

	var records []map[string]any
	dec := json.NewDecoder(buf)
	for dec.More() {
		var rec map[string]any
		if err := dec.Decode(&rec); err != nil {
			t.Fatalf("decode log: %v", err)
		}
		records = append(records, rec)
	}
	return records
}

// Ловит две регрессии: usecase перестал писать обязательное Info-событие «trip completed»
// (или потерял в нём trip_id) и запись попала в лог до commit, то есть для поездки,
// которая по итогу не завершена.
func TestUseCaseFinishLogsOnlyAfterCommit(t *testing.T) {
	id := uuid.New()
	errCommit := errors.New("commit failed")

	tests := []struct {
		name      string
		commitErr error
		wantLog   bool
	}{
		{name: "completed trip is logged with its trip_id", wantLog: true},
		{name: "commit failure writes no trip completed record", commitErr: errCommit},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			trips := NewMocktripRepository(ctrl)
			history := NewMockhistoryRepository(ctrl)
			txManager := NewMocktxManager(ctrl)
			metrics := NewMocktripMetrics(ctrl)

			trips.EXPECT().GetForUpdate(gomock.Any(), id).
				Return(&trip.Trip{ID: id, Status: trip.StatusActive}, nil)
			trips.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
			history.EXPECT().Create(gomock.Any(), id, gomock.Any(), trip.StatusCompleted).Return(nil)
			txManager.EXPECT().Do(gomock.Any(), gomock.Any()).DoAndReturn(commitFunc(tt.commitErr))
			if tt.wantLog {
				metrics.EXPECT().TripCompleted(gomock.Any())
			}

			var buf bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&buf, nil))

			_, err := finish.New(trips, history, txManager, metrics, logger).Finish(context.Background(), id)
			if !errors.Is(err, tt.commitErr) {
				t.Fatalf("Finish() error = %v, want %v", err, tt.commitErr)
			}

			var found map[string]any
			for _, rec := range logRecords(t, &buf) {
				if rec["msg"] == "trip completed" {
					found = rec
				}
			}

			if !tt.wantLog {
				if found != nil {
					t.Errorf("log must have no trip completed record when commit failed, got %v", found)
				}
				return
			}
			if found == nil {
				t.Fatalf("no \"trip completed\" record in log:\n%s", buf.String())
			}
			if found["level"] != "INFO" {
				t.Errorf("level = %v, want INFO", found["level"])
			}
			if found["trip_id"] != id.String() {
				t.Errorf("trip_id = %v, want %s", found["trip_id"], id)
			}
		})
	}
}
