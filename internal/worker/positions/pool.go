package positions

import (
	"container/heap"
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

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
	closed bool
}

func NewPool(ctx context.Context, push pushGateway, workers, capacity int, requestTimeout time.Duration, log *slog.Logger) (*Pool, error) {
	if workers < 1 || capacity < 1 || requestTimeout <= 0 {
		return nil, fmt.Errorf("position workers, queue capacity and request timeout must be positive")
	}
	ctx, cancel := context.WithCancel(ctx)
	p := &Pool{
		push: push, requestTimeout: requestTimeout, capacity: capacity, log: log,
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

func (p *Pool) Enqueue(candidate trip.StaleTrip) EnqueueResult {
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
	item := &task{trip: candidate}
	heap.Push(&p.queue, item)
	p.tasks[candidate.TripID] = item
	p.ready.Signal()
	return result
}

func (p *Pool) Cancel(tripID uuid.UUID) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	item := p.tasks[tripID]
	if item == nil || item.index < 0 {
		return false
	}
	p.remove(item)
	return true
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
	return heap.Pop(&p.queue).(*task)
}

func (p *Pool) work(ctx context.Context) {
	for {
		item := p.take(ctx)
		if item == nil {
			return
		}
		requestCtx, cancel := context.WithTimeout(ctx, p.requestTimeout)
		err := p.push.RequestPosition(requestCtx, item.trip.DriverID, item.trip.TripID)
		cancel()
		if err != nil {
			p.log.WarnContext(ctx, "request position failed", slog.String("trip_id", item.trip.TripID.String()), slog.Any("error", err))
		}
		p.mu.Lock()
		if p.tasks[item.trip.TripID] == item {
			delete(p.tasks, item.trip.TripID)
		}
		p.mu.Unlock()
	}
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
