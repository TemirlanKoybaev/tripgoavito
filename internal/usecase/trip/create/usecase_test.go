package create_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"testing"
	"time"

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

func newInput() create.Input {
	return create.Input{
		UserID:         uuid.New(),
		DriverID:       uuid.New(),
		StartLatitude:  59.9398,
		StartLongitude: 30.3146,
		EndLatitude:    59.929,
		EndLongitude:   30.3626,
		Price:          1450,
	}
}

func TestUseCaseCreate(t *testing.T) {
	in := newInput()
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

// Ловит регрессию «usecase собрал поездку не целиком»: потерял координаты или StartedAt.
// Проверяем именно то, что получил репозиторий, а не только ответ usecase: в БД уходит
// объект из trips.Create, и unit-тест хендлера или интеграционный тест репозитория
// эту сборку не покрывают.
func TestUseCaseCreateSavesWholeTrip(t *testing.T) {
	in := newInput()
	ctrl := gomock.NewController(t)
	trips := NewMocktripRepository(ctrl)
	history := NewMockhistoryRepository(ctrl)
	txManager := NewMocktxManager(ctrl)
	metrics := NewMocktripMetrics(ctrl)

	var saved trip.Trip
	trips.EXPECT().Create(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, tr *trip.Trip) error {
			saved = *tr
			return nil
		})
	history.EXPECT().Create(gomock.Any(), gomock.Any(), gomock.Nil(), trip.StatusActive).Return(nil)
	txManager.EXPECT().Do(gomock.Any(), gomock.Any()).DoAndReturn(commitFunc(nil))
	metrics.EXPECT().TripCreated(gomock.Any())

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	before := time.Now()
	got, err := create.New(trips, history, txManager, metrics, logger).Create(context.Background(), in)
	after := time.Now()
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// StartedAt проверяем отдельно: он берётся из часов и должен лежать между before и after.
	startedAt := saved.StartedAt
	if startedAt.IsZero() || startedAt.Before(before.Truncate(time.Microsecond)) || startedAt.After(after) {
		t.Errorf("StartedAt = %v, want a time between %v and %v", startedAt, before, after)
	}
	saved.StartedAt = time.Time{}

	want := trip.Trip{
		ID:             saved.ID,
		UserID:         in.UserID,
		DriverID:       in.DriverID,
		StartLatitude:  in.StartLatitude,
		StartLongitude: in.StartLongitude,
		EndLatitude:    in.EndLatitude,
		EndLongitude:   in.EndLongitude,
		Price:          in.Price,
		Status:         trip.StatusActive,
	}
	if saved.ID == uuid.Nil {
		t.Error("repository got a trip without ID")
	}
	if !reflect.DeepEqual(saved, want) {
		t.Errorf("repository got %+v, want %+v", saved, want)
	}
	if got.ID != saved.ID {
		t.Errorf("returned trip ID = %v, want the saved one %v", got.ID, saved.ID)
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

// Ловит две регрессии: usecase перестал писать обязательное Info-событие «trip created»
// (или потерял в нём trip_id) и запись попала в лог до commit, то есть для поездки,
// которой по итогу нет.
func TestUseCaseCreateLogsOnlyAfterCommit(t *testing.T) {
	errCommit := errors.New("commit failed")

	tests := []struct {
		name      string
		commitErr error
		wantLog   bool
	}{
		{name: "created trip is logged with its trip_id", wantLog: true},
		{name: "commit failure writes no trip created record", commitErr: errCommit},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			trips := NewMocktripRepository(ctrl)
			history := NewMockhistoryRepository(ctrl)
			txManager := NewMocktxManager(ctrl)
			metrics := NewMocktripMetrics(ctrl)

			trips.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
			history.EXPECT().Create(gomock.Any(), gomock.Any(), gomock.Nil(), trip.StatusActive).Return(nil)
			txManager.EXPECT().Do(gomock.Any(), gomock.Any()).DoAndReturn(commitFunc(tt.commitErr))
			if tt.wantLog {
				metrics.EXPECT().TripCreated(gomock.Any())
			}

			var buf bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&buf, nil))

			got, err := create.New(trips, history, txManager, metrics, logger).Create(context.Background(), newInput())
			if !errors.Is(err, tt.commitErr) {
				t.Fatalf("Create() error = %v, want %v", err, tt.commitErr)
			}

			var found map[string]any
			for _, rec := range logRecords(t, &buf) {
				if rec["msg"] == "trip created" {
					found = rec
				}
			}

			if !tt.wantLog {
				if found != nil {
					t.Errorf("log must have no trip created record when commit failed, got %v", found)
				}
				return
			}
			if found == nil {
				t.Fatalf("no \"trip created\" record in log:\n%s", buf.String())
			}
			if found["level"] != "INFO" {
				t.Errorf("level = %v, want INFO", found["level"])
			}
			if found["trip_id"] != got.ID.String() {
				t.Errorf("trip_id = %v, want %s", found["trip_id"], got.ID)
			}
		})
	}
}
