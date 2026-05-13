package resilience

import (
	"context"
	"math/rand"
	"time"
)

// MoyskladHTTPRetrySleepDuration — длительность ожидания без привязки к context.
func MoyskladHTTPRetrySleepDuration(attempt int) time.Duration {
	base := time.Second
	shift := attempt
	if shift > 8 {
		shift = 8
	}
	exp := base * time.Duration(1<<uint(shift))
	if exp > 30*time.Minute {
		exp = 30 * time.Minute
	}
	jitter := time.Duration(rand.Int63n(int64(exp/8 + 1)))
	return exp + jitter
}

// MoyskladHTTPRetrySleep — задержка между попытками HTTP к МойСклад (экспонента + jitter).
func MoyskladHTTPRetrySleep(ctx context.Context, attempt int) error {
	return SleepCtx(ctx, MoyskladHTTPRetrySleepDuration(attempt))
}
