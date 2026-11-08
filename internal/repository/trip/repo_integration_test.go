//go:build integration

package trip_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/infra/postgres"
	"github.com/TemirlanKoybaev/tripgoavito.git/internal/infra/postgres/transaction"
	historyrepo "github.com/TemirlanKoybaev/tripgoavito.git/internal/repository/history"
	triprepo "github.com/TemirlanKoybaev/tripgoavito.git/internal/repository/trip"
)

const queryTimeout = 3 * time.Second

type env struct {
	pool    *pgxpool.Pool
	trips   *triprepo.Repository
	history *historyrepo.Repository
	tx      transaction.TxManager
}

func newEnv(t *testing.T) *env {
	t.Helper()

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Fatal("DATABASE_URL is not set, run make test-integration")
	}

	pool, err := postgres.Connect(context.Background(), url, 5, 1, time.Minute, 5*time.Second, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	return &env{
		pool:    pool,
		trips:   triprepo.New(pool, queryTimeout),
		history: historyrepo.New(pool, queryTimeout),
		tx:      transaction.New(pool),
	}
}

func (e *env) newTrip(t *testing.T) *domain.Trip {
	t.Helper()

	tr := &domain.Trip{
		ID:             uuid.New(),
		UserID:         uuid.New(),
		DriverID:       uuid.New(),
		StartLatitude:  59.9398,
		StartLongitude: 30.3146,
		EndLatitude:    59.929,
		EndLongitude:   30.3626,
		Price:          1450,
		Status:         domain.StatusActive,
		StartedAt:      time.Now().UTC().Truncate(time.Microsecond),
	}

	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = e.pool.Exec(ctx, "DELETE FROM trip_status_history WHERE trip_id = $1", tr.ID)
		_, _ = e.pool.Exec(ctx, "DELETE FROM trips WHERE id = $1", tr.ID)
	})

	return tr
}

func (e *env) count(t *testing.T, query string, id uuid.UUID) int {
	t.Helper()

	var n int
	if err := e.pool.QueryRow(context.Background(), query, id).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestCreateAndGetByID(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	want := e.newTrip(t)

	if err := e.trips.Create(ctx, want); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	got, err := e.trips.GetByID(ctx, want.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}

	if got.ID != want.ID || got.UserID != want.UserID || got.DriverID != want.DriverID {
		t.Errorf("ids = %+v, want %+v", got, want)
	}
	if got.StartLatitude != want.StartLatitude || got.StartLongitude != want.StartLongitude ||
		got.EndLatitude != want.EndLatitude || got.EndLongitude != want.EndLongitude {
		t.Errorf("coordinates = %+v, want %+v", got, want)
	}
	if got.Price != want.Price || got.Status != domain.StatusActive || got.FinishedAt != nil {
		t.Errorf("trip = %+v, want active trip with price %d", got, want.Price)
	}
	if !got.StartedAt.Equal(want.StartedAt) {
		t.Errorf("started_at = %v, want %v", got.StartedAt, want.StartedAt)
	}
}

func TestGetByIDNotFound(t *testing.T) {
	e := newEnv(t)

	_, err := e.trips.GetByID(context.Background(), uuid.New())

	if !errors.Is(err, domain.ErrTripNotFound) {
		t.Fatalf("GetByID() error = %v, want %v", err, domain.ErrTripNotFound)
	}
}

func TestCreateDriverBusy(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	first := e.newTrip(t)
	second := e.newTrip(t)
	second.DriverID = first.DriverID

	if err := e.trips.Create(ctx, first); err != nil {
		t.Fatalf("Create(first) error = %v", err)
	}

	err := e.trips.Create(ctx, second)
	if !errors.Is(err, domain.ErrDriverBusy) {
		t.Fatalf("Create(second) error = %v, want %v", err, domain.ErrDriverBusy)
	}

	if err := first.Finish(time.Now().UTC().Truncate(time.Microsecond)); err != nil {
		t.Fatalf("Finish() error = %v", err)
	}
	if err := e.trips.Update(ctx, first); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	if err := e.trips.Create(ctx, second); err != nil {
		t.Fatalf("Create(second) after finish error = %v", err)
	}
}

func TestUpdate(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	tr := e.newTrip(t)

	if err := e.trips.Create(ctx, tr); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	finishedAt := time.Now().UTC().Truncate(time.Microsecond)
	if err := tr.Finish(finishedAt); err != nil {
		t.Fatalf("Finish() error = %v", err)
	}
	if err := e.trips.Update(ctx, tr); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	got, err := e.trips.GetByID(ctx, tr.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got.Status != domain.StatusCompleted {
		t.Errorf("status = %q, want %q", got.Status, domain.StatusCompleted)
	}
	if got.FinishedAt == nil || !got.FinishedAt.Equal(finishedAt) {
		t.Errorf("finished_at = %v, want %v", got.FinishedAt, finishedAt)
	}
}

func TestUpdateNotFound(t *testing.T) {
	e := newEnv(t)
	tr := e.newTrip(t)

	err := e.trips.Update(context.Background(), tr)

	if !errors.Is(err, domain.ErrTripNotFound) {
		t.Fatalf("Update() error = %v, want %v", err, domain.ErrTripNotFound)
	}
}

func TestTransactionCommit(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	tr := e.newTrip(t)

	err := e.tx.Do(ctx, func(ctx context.Context) error {
		if err := e.trips.Create(ctx, tr); err != nil {
			return err
		}
		return e.history.Create(ctx, tr.ID, nil, domain.StatusActive)
	})
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}

	if n := e.count(t, "SELECT count(*) FROM trips WHERE id = $1", tr.ID); n != 1 {
		t.Errorf("trips rows = %d, want 1", n)
	}
	if n := e.count(t, "SELECT count(*) FROM trip_status_history WHERE trip_id = $1", tr.ID); n != 1 {
		t.Errorf("history rows = %d, want 1", n)
	}
}

func TestTransactionRollback(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	tr := e.newTrip(t)
	boom := errors.New("second step failed")

	err := e.tx.Do(ctx, func(ctx context.Context) error {
		if err := e.trips.Create(ctx, tr); err != nil {
			return err
		}
		if err := e.history.Create(ctx, tr.ID, nil, domain.StatusActive); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Do() error = %v, want %v", err, boom)
	}

	if n := e.count(t, "SELECT count(*) FROM trips WHERE id = $1", tr.ID); n != 0 {
		t.Errorf("trips rows = %d, want 0 after rollback", n)
	}
	if n := e.count(t, "SELECT count(*) FROM trip_status_history WHERE trip_id = $1", tr.ID); n != 0 {
		t.Errorf("history rows = %d, want 0 after rollback", n)
	}
}

// База общая с другими тестами и сервисом, поэтому сравниваем с исходным значением.
func TestCountActive(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	before, err := e.trips.CountActive(ctx)
	if err != nil {
		t.Fatalf("CountActive() error = %v", err)
	}

	tr := e.newTrip(t)
	if err := e.trips.Create(ctx, tr); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	afterCreate, err := e.trips.CountActive(ctx)
	if err != nil {
		t.Fatalf("CountActive() error = %v", err)
	}
	if afterCreate != before+1 {
		t.Errorf("after create = %d, want %d", afterCreate, before+1)
	}

	if err := tr.Finish(time.Now().UTC().Truncate(time.Microsecond)); err != nil {
		t.Fatalf("Finish() error = %v", err)
	}
	if err := e.trips.Update(ctx, tr); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	afterFinish, err := e.trips.CountActive(ctx)
	if err != nil {
		t.Fatalf("CountActive() error = %v", err)
	}
	if afterFinish != before {
		t.Errorf("after finish = %d, want %d", afterFinish, before)
	}
}
