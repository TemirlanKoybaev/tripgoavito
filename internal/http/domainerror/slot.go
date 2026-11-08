package domainerror

import (
	"context"
	"sync"
)

type slotKey struct{}

// Slot хранит ошибку, которую обработчик не отдаёт клиенту (внутренняя причина
// 5xx, сбой записи ответа), но которую нужно увидеть в логе запроса.
// Так логгер не приходится протаскивать в каждый хендлер: запись
// «request handled» делает одно место — middleware Logging.
type Slot struct {
	mu  sync.Mutex
	err error
}

// WithSlot кладёт в контекст пустой слот и возвращает его.
func WithSlot(ctx context.Context) (context.Context, *Slot) {
	s := &Slot{}
	return context.WithValue(ctx, slotKey{}, s), s
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
