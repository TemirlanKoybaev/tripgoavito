package positions

import (
	"container/heap"
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
)

type EnqueueStatus string

const (
	Queued    EnqueueStatus = "queued"
	Duplicate EnqueueStatus = "duplicate"
	Dropped   EnqueueStatus = "dropped"
)

type EnqueueResult struct {
	Status  EnqueueStatus
	Evicted *trip.StaleTrip
}

type Pool struct {
	push           pushGateway
	metrics        poolMetrics
	requestTimeout time.Duration
	capacity       int
	log            *slog.Logger
	runCtx         context.Context
	cancel         context.CancelFunc
	done           chan struct{}

	mu     sync.Mutex
	ready  *sync.Cond
	queue  priorityQueue
	tasks  map[uuid.UUID]*task
	active int64
	closed bool
}

func NewPool(ctx context.Context, push pushGateway, metrics poolMetrics, workers, capacity int, requestTimeout time.Duration, log *slog.Logger) (*Pool, error) {
	if workers < 1 || capacity < 1 || requestTimeout <= 0 {
		return nil, fmt.Errorf("position workers, queue capacity and request timeout must be positive")
	}
	ctx, cancel := context.WithCancel(ctx)
	p := &Pool{
		push: push, metrics: metrics, requestTimeout: requestTimeout, capacity: capacity, log: log,
		runCtx: ctx, cancel: cancel, done: make(chan struct{}), tasks: make(map[uuid.UUID]*task),
	}
	p.ready = sync.NewCond(&p.mu)
	stopAbort := context.AfterFunc(ctx, p.abort)
	p.log.InfoContext(ctx, "position workers started", slog.Int("workers", workers))
	var group sync.WaitGroup
	for range workers {
		group.Go(func() { p.work(ctx) })
	}
	go func() {
		group.Wait()
		stopAbort()
		cancel()
		p.log.InfoContext(ctx, "position workers stopped")
		close(p.done)
	}()
	return p, nil
}

func (p *Pool) Enqueue(ctx context.Context, candidate trip.StaleTrip) EnqueueResult {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.runCtx.Err() != nil {
		return EnqueueResult{Status: Dropped}
	}
	if _, ok := p.tasks[candidate.TripID]; ok {
		return EnqueueResult{Status: Duplicate}
	}
	result := EnqueueResult{Status: Queued}
	if len(p.queue) == p.capacity {
		latest := p.queue.latest()
		if !candidate.EffectiveAt.Before(latest.trip.EffectiveAt) {
			return EnqueueResult{Status: Dropped}
		}
		p.remove(latest)
		evicted := latest.trip
		result.Evicted = &evicted
	}
	item := &task{trip: candidate, scanSpan: trace.SpanContextFromContext(ctx)}
	heap.Push(&p.queue, item)
	p.tasks[candidate.TripID] = item
	p.ready.Signal()
	return result
}

func (p *Pool) Cancel(ctx context.Context, tripID uuid.UUID) bool {
	p.mu.Lock()
	item := p.tasks[tripID]
	if item == nil || item.index < 0 {
		p.mu.Unlock()
		return false
	}
	p.remove(item)
	p.mu.Unlock()
	p.metrics.PositionRequest(ctx, "cancelled")
	return true
}

func (p *Pool) Stats() (queued, active int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return int64(len(p.queue)), p.active
}

func (p *Pool) remove(item *task) {
	heap.Remove(&p.queue, item.index)
	delete(p.tasks, item.trip.TripID)
}

func (p *Pool) take(ctx context.Context) *task {
	p.mu.Lock()
	defer p.mu.Unlock()
	for len(p.queue) == 0 && !p.closed && ctx.Err() == nil {
		p.ready.Wait()
	}
	if len(p.queue) == 0 || ctx.Err() != nil {
		return nil
	}
	p.active++
	return heap.Pop(&p.queue).(*task)
}

func (p *Pool) work(ctx context.Context) {
	for {
		item := p.take(ctx)
		if item == nil {
			return
		}
		p.process(ctx, item)
		p.mu.Lock()
		p.active--
		if p.tasks[item.trip.TripID] == item {
			delete(p.tasks, item.trip.TripID)
		}
		p.mu.Unlock()
	}
}

func (p *Pool) process(ctx context.Context, item *task) {
	ctx, span := otel.Tracer(tracerName).Start(ctx, "positions.process", trace.WithNewRoot(),
		trace.WithLinks(trace.Link{SpanContext: item.scanSpan}),
	)
	defer span.End()
	requestCtx, cancel := context.WithTimeout(ctx, p.requestTimeout)
	defer cancel()
	started := time.Now()
	err := p.push.RequestPosition(requestCtx, item.trip.DriverID, item.trip.TripID)
	duration := time.Since(started)
	result := "success"
	if err != nil {
		result = "error"
		span.RecordError(err)
		span.SetStatus(codes.Error, "push request failed")
		p.log.WarnContext(ctx, "request position failed", slog.String("trip_id", item.trip.TripID.String()), slog.Any("error", err))
	}
	p.metrics.PositionRequest(ctx, result)
	p.metrics.RecordPositionRequestDuration(ctx, duration, result)
}

// Shutdown закрывает приём и даёт воркерам разобрать очередь до дедлайна.
// Истечение дедлайна отменяет выполняемые запросы и очищает ожидающие задачи.
func (p *Pool) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	p.closed = true
	p.ready.Broadcast()
	p.mu.Unlock()

	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
		p.cancel()
		p.abort()
		return ctx.Err()
	}
}

func (p *Pool) abort() {
	p.mu.Lock()
	p.closed = true
	p.queue = nil
	p.tasks = nil
	p.ready.Broadcast()
	p.mu.Unlock()
}
