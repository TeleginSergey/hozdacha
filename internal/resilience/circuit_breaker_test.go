package resilience

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	b := NewCircuitBreaker(3, time.Minute)

	if !b.Allow() {
		t.Fatal("breaker should start closed (Allow=true)")
	}
	for i := 0; i < 3; i++ {
		b.OnFailure()
	}
	if b.Allow() {
		t.Fatal("breaker should be OPEN after reaching fail threshold")
	}
}

func TestCircuitBreaker_HalfOpenAndRecovers(t *testing.T) {
	if testing.Short() {
		t.Skip("uses real time delay; skipped in -short")
	}
	// openTimeout клампится конструктором до минимума 1с — берём ровно 1с.
	b := NewCircuitBreaker(2, time.Second)
	b.OnFailure()
	b.OnFailure()
	if b.Allow() {
		t.Fatal("should be open right after threshold")
	}

	time.Sleep(1100 * time.Millisecond)
	if !b.Allow() {
		t.Fatal("should allow a probe request in half-open state")
	}
	// Успешный probe закрывает breaker.
	b.OnSuccess()
	if !b.Allow() {
		t.Fatal("breaker should be closed after successful probe")
	}
}

func TestCircuitBreaker_Call(t *testing.T) {
	b := NewCircuitBreaker(2, time.Minute)
	failing := func() error { return errors.New("boom") }

	if err := b.Call(context.Background(), failing); err == nil {
		t.Fatal("Call should propagate the function error")
	}
	_ = b.Call(context.Background(), failing) // второй сбой → breaker открывается

	// Теперь breaker открыт: Call должен вернуть ErrCircuitOpen, не вызывая fn.
	called := false
	err := b.Call(context.Background(), func() error { called = true; return nil })
	if !errors.Is(err, ErrCircuitOpen) {
		t.Errorf("err = %v, want ErrCircuitOpen", err)
	}
	if called {
		t.Error("fn must not be called while breaker is open")
	}
}

// Экспоненциальный backoff для повторных попыток обработки вебхука.
func TestWebhookRetryDelay_Exponential(t *testing.T) {
	prev := WebhookRetryDelay(0)
	for attempt := 1; attempt < 5; attempt++ {
		cur := WebhookRetryDelay(attempt)
		if cur < prev {
			t.Errorf("delay for attempt %d (%v) < previous (%v): backoff must not decrease", attempt, cur, prev)
		}
		prev = cur
	}
}
