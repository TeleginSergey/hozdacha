package resilience

import (
	"context"
	"testing"
	"time"
)

func TestMoyskladHTTPRetrySleepDuration_Grows(t *testing.T) {
	prev := MoyskladHTTPRetrySleepDuration(0)
	for attempt := 1; attempt < 5; attempt++ {
		cur := MoyskladHTTPRetrySleepDuration(attempt)
		if cur <= 0 {
			t.Fatalf("attempt %d: non-positive delay %v", attempt, cur)
		}
		if cur < prev {
			t.Errorf("attempt %d: delay %v decreased from %v", attempt, cur, prev)
		}
		prev = cur
	}
}

func TestMoyskladHTTPRetrySleep_RespectsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // отменён сразу
	if err := MoyskladHTTPRetrySleep(ctx, 1); err == nil {
		t.Error("expected context cancellation error")
	}
}

func TestMoyskladHTTPRetrySleep_Sleeps(t *testing.T) {
	if testing.Short() {
		t.Skip("uses real time; skipped in -short")
	}
	start := time.Now()
	if err := MoyskladHTTPRetrySleep(context.Background(), 0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if time.Since(start) <= 0 {
		t.Error("sleep returned instantly")
	}
}
