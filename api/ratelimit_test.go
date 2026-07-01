package api

import (
	"testing"
	"time"
)

func TestIPRateLimiterBurstThenThrottle(t *testing.T) {
	l := newIPRateLimiter(1.0, 3)
	now := time.Unix(1_700_000_000, 0)

	for i := 0; i < 3; i++ {
		if !l.allow("1.2.3.4", now) {
			t.Fatalf("request %d in burst should be allowed", i+1)
		}
	}
	if l.allow("1.2.3.4", now) {
		t.Fatalf("request beyond burst should be throttled")
	}

	now = now.Add(time.Second)
	if !l.allow("1.2.3.4", now) {
		t.Fatalf("one token should have refilled after 1s")
	}
	if l.allow("1.2.3.4", now) {
		t.Fatalf("only one token should refill per second")
	}
}

func TestIPRateLimiterIsolatesClients(t *testing.T) {
	l := newIPRateLimiter(1.0, 2)
	now := time.Unix(1_700_000_000, 0)

	if !l.allow("10.0.0.1", now) || !l.allow("10.0.0.1", now) {
		t.Fatalf("IP A burst should be allowed")
	}
	if l.allow("10.0.0.1", now) {
		t.Fatalf("IP A should be throttled after burst")
	}
	if !l.allow("10.0.0.2", now) {
		t.Fatalf("IP B should be allowed regardless of IP A")
	}
}
