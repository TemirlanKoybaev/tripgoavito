package positions

import (
	"bytes"

	"go.opentelemetry.io/otel/trace"

	"github.com/TemirlanKoybaev/tripgoavito.git/internal/domain/trip"
)

type task struct {
	trip     trip.StaleTrip
	scanSpan trace.SpanContext
	index    int // -1, когда задача уже не в очереди.
}

type priorityQueue []*task

func (q priorityQueue) Len() int { return len(q) }

func (q priorityQueue) Less(i, j int) bool {
	a, b := q[i].trip, q[j].trip
	if a.EffectiveAt.Equal(b.EffectiveAt) {
		return bytes.Compare(a.TripID[:], b.TripID[:]) < 0
	}
	return a.EffectiveAt.Before(b.EffectiveAt)
}

func (q priorityQueue) Swap(i, j int) {
	q[i], q[j] = q[j], q[i]
	q[i].index, q[j].index = i, j
}

func (q *priorityQueue) Push(value any) {
	item := value.(*task)
	item.index = len(*q)
	*q = append(*q, item)
}

func (q *priorityQueue) Pop() any {
	last := len(*q) - 1
	item := (*q)[last]
	item.index = -1
	(*q)[last] = nil
	*q = (*q)[:last]
	return item
}

func (q priorityQueue) latest() *task {
	// Максимум мин-кучи лежит среди её листьев.
	latest := len(q) / 2
	for i := latest + 1; i < len(q); i++ {
		if q.Less(latest, i) {
			latest = i
		}
	}
	return q[latest]
}
