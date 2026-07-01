package syncloop

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunStopsWhenStopChannelCloses(t *testing.T) {
	stop := make(chan struct{})
	var calls atomic.Int64

	Run(stop, 5*time.Millisecond, "test", func() error {
		calls.Add(1)
		return nil
	})

	time.Sleep(30 * time.Millisecond)
	close(stop)
	time.Sleep(20 * time.Millisecond)
	after := calls.Load()
	if after == 0 {
		t.Fatal("sync function never ran")
	}

	time.Sleep(40 * time.Millisecond)
	if calls.Load() != after {
		t.Fatalf("sync kept running after stop: %d -> %d", after, calls.Load())
	}
}

func TestRunBacksOffOnConsecutiveFailures(t *testing.T) {
	stop := make(chan struct{})
	defer close(stop)
	var calls atomic.Int64

	Run(stop, 10*time.Millisecond, "test", func() error {
		calls.Add(1)
		return errors.New("API returned status 429")
	})

	time.Sleep(100 * time.Millisecond)
	got := calls.Load()
	if got == 0 {
		t.Fatal("sync function never ran")
	}
	if got > 5 {
		t.Fatalf("expected backoff to throttle failing sync, got %d calls in 100ms", got)
	}
}

func TestRunRecoversIntervalAfterSuccess(t *testing.T) {
	stop := make(chan struct{})
	defer close(stop)
	var calls atomic.Int64

	Run(stop, 5*time.Millisecond, "test", func() error {
		n := calls.Add(1)
		if n <= 2 {
			return errors.New("transient")
		}
		return nil
	})

	time.Sleep(150 * time.Millisecond)
	if got := calls.Load(); got < 8 {
		t.Fatalf("expected interval to reset after success, got only %d calls", got)
	}
}
