package positions

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"

	"github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
)

const tracerName = "tripgo/positions"

type systemTicker struct{ *time.Ticker }

func NewTicker(interval time.Duration) *systemTicker {
	return &systemTicker{Ticker: time.NewTicker(interval)}
}

func (t *systemTicker) Ticks() <-chan time.Time { return t.C }

type Scanner struct {
	trips      staleTripRepository
	pool       *Pool
	metrics    scannerMetrics
	staleAfter time.Duration
	batchSize  int
	log        *slog.Logger
	cancel     context.CancelFunc
	done       chan struct{}
}

func NewScanner(ctx context.Context, trips staleTripRepository, pool *Pool, metrics scannerMetrics, ticker scanTicker, staleAfter time.Duration, batchSize int, log *slog.Logger) *Scanner {
	ctx, cancel := context.WithCancel(ctx)
	s := &Scanner{
		trips: trips, pool: pool, metrics: metrics, staleAfter: staleAfter, batchSize: batchSize,
		log: log, cancel: cancel, done: make(chan struct{}),
	}
	go s.run(ctx, ticker)
	return s
}

func (s *Scanner) run(ctx context.Context, ticker scanTicker) {
	defer close(s.done)
	defer s.cancel()
	defer ticker.Stop()
	s.log.InfoContext(ctx, "position scanner started")
	defer s.log.InfoContext(ctx, "position scanner stopped")
	ticks := ticker.Ticks()
	for {
		select {
		case <-ctx.Done():
			return
		case tick, ok := <-ticks:
			if !ok || ctx.Err() != nil {
				return
			}
			s.scan(ctx, tick.Add(-s.staleAfter))
		}
	}
}

func (s *Scanner) scan(ctx context.Context, cutoff time.Time) {
	ctx, span := otel.Tracer(tracerName).Start(ctx, "positions.scan")
	defer span.End()
	var rowsRead int64
	defer func() { s.metrics.RecordStaleRows(ctx, rowsRead) }()

	var after *trip.StaleTripCursor
	for ctx.Err() == nil {
		batch, err := s.trips.ListStaleTrips(ctx, cutoff, after, s.batchSize)
		if err != nil {
			if ctx.Err() == nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, "stale trip query failed")
				s.log.WarnContext(ctx, "list stale trips failed", slog.Any("error", err))
			}
			return
		}
		rowsRead += int64(len(batch))
		for _, candidate := range batch {
			if ctx.Err() != nil || !s.enqueue(ctx, candidate) {
				return
			}
		}
		if len(batch) < s.batchSize {
			return
		}
		last := batch[len(batch)-1]
		after = &trip.StaleTripCursor{EffectiveAt: last.EffectiveAt, TripID: last.TripID}
	}
}

func (s *Scanner) enqueue(ctx context.Context, candidate trip.StaleTrip) bool {
	result := s.pool.Enqueue(ctx, candidate)
	s.metrics.StaleCheck(ctx, string(result.Status))
	if result.Evicted != nil {
		s.metrics.StaleCheck(ctx, string(Dropped))
		s.log.WarnContext(ctx, "position request evicted", slog.String("trip_id", result.Evicted.TripID.String()))
		return false
	}
	if result.Status == Dropped {
		s.log.WarnContext(ctx, "position request dropped", slog.String("trip_id", candidate.TripID.String()))
		return false
	}
	return true
}

func (s *Scanner) Shutdown(ctx context.Context) error {
	s.cancel()
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
