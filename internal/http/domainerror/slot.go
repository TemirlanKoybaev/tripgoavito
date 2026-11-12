package domainerror

import (
	"context"
	"sync"
)

type slotKey struct{}

// Slot хранит то, что обработчик не отдаёт клиенту, но что нужно наблюдаемости:
// внутреннюю причину 5xx и сбой записи ответа (err, для лога запроса) и
// низкокардинальный тип ошибки (errType, для error.type на спане).
// Так логгер и трейсер не приходится протаскивать в каждый хендлер: лог пишет
// middleware Logging, спан размечает middleware Tracing.
type Slot struct {
	mu      sync.Mutex
	err     error
	errType string
}

// WithSlot возвращает контекст со слотом. Если слот уже есть (его создал
// внешний middleware), берёт существующий, чтобы все видели одни данные.
func WithSlot(ctx context.Context) (context.Context, *Slot) {
	if s, _ := ctx.Value(slotKey{}).(*Slot); s != nil {
		return ctx, s
	}
	s := &Slot{}
	return context.WithValue(ctx, slotKey{}, s), s
}

// RecordType запоминает тип ошибки запроса (код проблемы, например trip_not_found).
// Берём код, а не текст ошибки: текст высококардинален и в атрибуты не годится.
func RecordType(ctx context.Context, errType string) {
	s, _ := ctx.Value(slotKey{}).(*Slot)
	if s == nil || errType == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.errType == "" {
		s.errType = errType
	}
}

func (s *Slot) Type() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.errType
}

// Record запоминает первую ошибку запроса. Без слота в контексте ничего не делает.
func Record(ctx context.Context, err error) {
	s, _ := ctx.Value(slotKey{}).(*Slot)
	if s == nil || err == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err == nil {
		s.err = err
	}
}

func (s *Slot) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}
