package handlers

import (
	"sync"
	"time"
)

// attemptTracker — потокобезопасный счётчик попыток по ключу (например, email)
// с автоматической очисткой устаревших записей, чтобы карта не росла бесконечно.
type attemptTracker struct {
	mu  sync.Mutex
	m   map[string]*attemptRec
	ttl time.Duration
}

type attemptRec struct {
	count int
	last  time.Time
}

func newAttemptTracker(ttl time.Duration) *attemptTracker {
	t := &attemptTracker{m: make(map[string]*attemptRec), ttl: ttl}
	go t.cleanupLoop()
	return t
}

func (t *attemptTracker) cleanupLoop() {
	for {
		time.Sleep(5 * time.Minute)
		t.mu.Lock()
		now := time.Now()
		for k, r := range t.m {
			if now.Sub(r.last) > t.ttl {
				delete(t.m, k)
			}
		}
		t.mu.Unlock()
	}
}

// inc увеличивает счётчик попыток для ключа и возвращает новое значение.
// Если предыдущая запись устарела (старше ttl), счёт начинается заново.
func (t *attemptTracker) inc(key string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	r, ok := t.m[key]
	if !ok || time.Since(r.last) > t.ttl {
		r = &attemptRec{}
		t.m[key] = r
	}
	r.count++
	r.last = time.Now()
	return r.count
}

// get возвращает текущий счётчик попыток (0 если записи нет или она устарела).
func (t *attemptTracker) get(key string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	r, ok := t.m[key]
	if !ok || time.Since(r.last) > t.ttl {
		return 0
	}
	return r.count
}

// reset сбрасывает счётчик для ключа (после успешной операции).
func (t *attemptTracker) reset(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.m, key)
}
