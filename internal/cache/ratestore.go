package cache

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// RateStore — счётчики с TTL для rate-limit и анти-брутфорса.
// При наличии Redis работает в нём (корректно при нескольких репликах),
// иначе — потокобезопасный in-memory фолбэк с авто-очисткой устаревших ключей.
type RateStore struct {
	rdb    *redis.Client
	logger *zap.Logger
	mem    *memCounter
}

// NewRateStore создаёт хранилище. Если rdb == nil — используется in-memory фолбэк.
func NewRateStore(rdb *redis.Client, logger *zap.Logger) *RateStore {
	s := &RateStore{rdb: rdb, logger: logger}
	if rdb == nil {
		s.mem = newMemCounter()
	}
	return s
}

// Allow реализует фиксированное окно: не более limit обращений за window по ключу.
func (s *RateStore) Allow(ctx context.Context, key string, limit int, window time.Duration) bool {
	return s.Incr(ctx, key, window) <= int64(limit)
}

// Incr увеличивает счётчик ключа и при первом инкременте выставляет TTL.
// Возвращает новое значение счётчика.
func (s *RateStore) Incr(ctx context.Context, key string, ttl time.Duration) int64 {
	if s.rdb != nil {
		n, err := s.rdb.Incr(ctx, key).Result()
		if err != nil {
			// Fail-open: при сбое Redis не блокируем (есть другие слои защиты).
			if s.logger != nil {
				s.logger.Warn("ratestore incr failed", zap.String("key", key), zap.Error(err))
			}
			return 1
		}
		if n == 1 {
			s.rdb.Expire(ctx, key, ttl)
		}
		return n
	}
	return s.mem.incr(key, ttl)
}

// Get возвращает текущее значение счётчика (0, если ключа нет или он истёк).
func (s *RateStore) Get(ctx context.Context, key string) int64 {
	if s.rdb != nil {
		n, err := s.rdb.Get(ctx, key).Int64()
		if err != nil {
			return 0
		}
		return n
	}
	return s.mem.get(key)
}

// Reset удаляет счётчик (после успешной операции).
func (s *RateStore) Reset(ctx context.Context, key string) {
	if s.rdb != nil {
		s.rdb.Del(ctx, key)
		return
	}
	s.mem.reset(key)
}

// ----- in-memory фолбэк -----

type memCounter struct {
	mu sync.Mutex
	m  map[string]*memRec
}

type memRec struct {
	count   int64
	expires time.Time
}

func newMemCounter() *memCounter {
	c := &memCounter{m: make(map[string]*memRec)}
	go c.cleanupLoop()
	return c
}

func (c *memCounter) cleanupLoop() {
	for {
		time.Sleep(5 * time.Minute)
		c.mu.Lock()
		now := time.Now()
		for k, r := range c.m {
			if now.After(r.expires) {
				delete(c.m, k)
			}
		}
		c.mu.Unlock()
	}
}

func (c *memCounter) incr(key string, ttl time.Duration) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	r, ok := c.m[key]
	if !ok || now.After(r.expires) {
		r = &memRec{expires: now.Add(ttl)}
		c.m[key] = r
	}
	r.count++
	return r.count
}

func (c *memCounter) get(key string) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.m[key]
	if !ok || time.Now().After(r.expires) {
		return 0
	}
	return r.count
}

func (c *memCounter) reset(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, key)
}
