package cache

import (
	"context"
	"testing"
	"time"
)

// In-memory ветка RateStore (Redis-ветка покрывается интеграционным тестом).

func TestRateStore_AllowEnforcesLimit(t *testing.T) {
	s := NewRateStore(nil, nil)
	ctx := context.Background()
	key := "test:allow"
	const limit = 3

	for i := 1; i <= limit; i++ {
		if !s.Allow(ctx, key, limit, time.Minute) {
			t.Fatalf("request %d should be allowed", i)
		}
	}
	if s.Allow(ctx, key, limit, time.Minute) {
		t.Fatal("request over the limit should be denied")
	}
}

func TestRateStore_IncrAndGet(t *testing.T) {
	s := NewRateStore(nil, nil)
	ctx := context.Background()
	key := "test:incr"

	if got := s.Get(ctx, key); got != 0 {
		t.Fatalf("initial Get = %d, want 0", got)
	}
	for i := int64(1); i <= 5; i++ {
		if n := s.Incr(ctx, key, time.Minute); n != i {
			t.Fatalf("Incr #%d returned %d", i, n)
		}
	}
	if got := s.Get(ctx, key); got != 5 {
		t.Errorf("Get = %d, want 5", got)
	}
}

func TestRateStore_Reset(t *testing.T) {
	s := NewRateStore(nil, nil)
	ctx := context.Background()
	key := "test:reset"
	s.Incr(ctx, key, time.Minute)
	s.Incr(ctx, key, time.Minute)
	s.Reset(ctx, key)
	if got := s.Get(ctx, key); got != 0 {
		t.Errorf("after Reset Get = %d, want 0", got)
	}
}

func TestRateStore_TTLExpiry(t *testing.T) {
	s := NewRateStore(nil, nil)
	ctx := context.Background()
	key := "test:ttl"

	s.Incr(ctx, key, 40*time.Millisecond)
	s.Incr(ctx, key, 40*time.Millisecond)
	if got := s.Get(ctx, key); got != 2 {
		t.Fatalf("Get = %d, want 2", got)
	}
	time.Sleep(60 * time.Millisecond)
	if got := s.Get(ctx, key); got != 0 {
		t.Errorf("after TTL Get = %d, want 0 (expired)", got)
	}
}

// Эмуляция анти-брутфорса кода подтверждения: после N неверных попыток счётчик
// достигает порога (далее обработчик инвалидирует код).
func TestRateStore_CodeBruteforceThreshold(t *testing.T) {
	s := NewRateStore(nil, nil)
	ctx := context.Background()
	key := "code_attempts:user@mail.ru"
	const maxAttempts = 5

	var last int64
	for i := 0; i < maxAttempts; i++ {
		last = s.Incr(ctx, key, 15*time.Minute)
	}
	if last != maxAttempts {
		t.Fatalf("attempts counter = %d, want %d", last, maxAttempts)
	}
	if s.Get(ctx, key) < maxAttempts {
		t.Error("threshold not reached")
	}
}
