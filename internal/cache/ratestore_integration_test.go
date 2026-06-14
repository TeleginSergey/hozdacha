//go:build integration

package cache

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// Интеграционный тест RateStore с реальным Redis (сервис-контейнер в CI).
// Проверяет именно Redis-ветку (INCR + EXPIRE), которую не покрывает in-memory unit-тест.
// Запуск: go test -tags=integration ./internal/cache/...  (нужен REDIS_ADDR, напр. localhost:6379)
func TestIntegration_RateStore_Redis(t *testing.T) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		t.Skip("REDIS_ADDR not set — пропускаем интеграционный тест")
	}
	ctx := context.Background()

	client := redis.NewClient(&redis.Options{Addr: addr})
	defer client.Close()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("redis ping: %v", err)
	}

	s := NewRateStore(client, nil)
	key := fmt.Sprintf("itest:ratestore:%d", time.Now().UnixNano())
	defer client.Del(ctx, key)

	if n := s.Incr(ctx, key, time.Minute); n != 1 {
		t.Fatalf("first Incr = %d, want 1", n)
	}
	if n := s.Incr(ctx, key, time.Minute); n != 2 {
		t.Fatalf("second Incr = %d, want 2", n)
	}
	if g := s.Get(ctx, key); g != 2 {
		t.Errorf("Get = %d, want 2", g)
	}

	// TTL должен быть выставлен (EXPIRE при первом инкременте).
	if ttl := client.TTL(ctx, key).Val(); ttl <= 0 {
		t.Errorf("TTL not set on key: %v", ttl)
	}

	s.Reset(ctx, key)
	if g := s.Get(ctx, key); g != 0 {
		t.Errorf("after Reset Get = %d, want 0", g)
	}

	// Лимит.
	limKey := fmt.Sprintf("itest:limit:%d", time.Now().UnixNano())
	defer client.Del(ctx, limKey)
	for i := 0; i < 3; i++ {
		if !s.Allow(ctx, limKey, 3, time.Minute) {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
	if s.Allow(ctx, limKey, 3, time.Minute) {
		t.Error("4th request over limit=3 should be denied")
	}
}
