package resilience

import (
	"context"
	"errors"
	"sync"
	"time"
)

// CircuitBreaker упрощённый трёхсостоянияный breaker для внешнего HTTP API.
type CircuitBreaker struct {
	mu              sync.Mutex
	failures        int
	successes       int // в half-open
	state           int // 0 closed, 1 open, 2 half-open
	openedAt        time.Time
	failThreshold   int
	successHalfOpen int
	openTimeout     time.Duration
}

const (
	cbClosed = iota
	cbOpen
	cbHalfOpen
)

func NewCircuitBreaker(failThreshold int, openTimeout time.Duration) *CircuitBreaker {
	if failThreshold < 1 {
		failThreshold = 5
	}
	if openTimeout < time.Second {
		openTimeout = 45 * time.Second
	}
	return &CircuitBreaker{
		failThreshold:   failThreshold,
		successHalfOpen: 1,
		openTimeout:     openTimeout,
	}
}

var ErrCircuitOpen = errors.New("circuit breaker is open")

func (b *CircuitBreaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	switch b.state {
	case cbClosed:
		return true
	case cbOpen:
		if now.Sub(b.openedAt) >= b.openTimeout {
			b.state = cbHalfOpen
			b.successes = 0
			return true
		}
		return false
	case cbHalfOpen:
		return true
	default:
		return true
	}
}

func (b *CircuitBreaker) OnSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case cbHalfOpen:
		b.successes++
		if b.successes >= b.successHalfOpen {
			b.state = cbClosed
			b.failures = 0
		}
	case cbClosed:
		b.failures = 0
	}
}

func (b *CircuitBreaker) OnFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case cbHalfOpen:
		b.state = cbOpen
		b.openedAt = time.Now()
		b.failures = b.failThreshold
	case cbClosed:
		b.failures++
		if b.failures >= b.failThreshold {
			b.state = cbOpen
			b.openedAt = time.Now()
		}
	}
}

func (b *CircuitBreaker) Call(ctx context.Context, fn func() error) error {
	if !b.Allow() {
		return ErrCircuitOpen
	}
	err := fn()
	if err != nil {
		b.OnFailure()
		return err
	}
	b.OnSuccess()
	return nil
}

// WebhookRetryDelay — экспоненциальный backoff с jitter (после неудачной обработки вебхука).
func WebhookRetryDelay(attempt int) time.Duration {
	steps := []time.Duration{
		5 * time.Second, 10 * time.Second, 20 * time.Second, 40 * time.Second, 80 * time.Second,
		5 * time.Minute, 15 * time.Minute, time.Hour, time.Hour, time.Hour,
	}
	idx := attempt - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(steps) {
		idx = len(steps) - 1
	}
	base := steps[idx]
	// jitter до 20% + небольшой фикс
	j := time.Duration(time.Now().UnixNano()%int64(base/5+1)) + time.Millisecond*50
	return base + j
}

// SleepCtx обёртка над time.After.
func SleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
