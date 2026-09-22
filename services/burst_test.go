package services

import (
	"sync"
	"testing"
	"time"
)

// The bucket holds a full batch and refills at the plan's rate, so a caller
// may arrive in a burst and then proceeds steadily.
func TestBurstAllowsADepthThenThrottles(t *testing.T) {
	b := NewBurstLimiters()
	now := time.Date(2026, time.September, 22, 12, 0, 0, 0, time.UTC)

	depth := BurstDepthFor(5)
	for i := 0; i < depth; i++ {
		if ok, _ := b.AllowN(1, 5, 1, now); !ok {
			t.Fatalf("call %d of the bucket's %d was refused", i+1, depth)
		}
	}
	ok, wait := b.AllowN(1, 5, 1, now)
	if ok {
		t.Fatalf("call %d in the same instant was allowed", depth+1)
	}
	if wait <= 0 || wait > time.Second {
		t.Errorf("wait = %v, want a fraction of a second", wait)
	}

	// A fifth of a second later one token has refilled, and only one.
	later := now.Add(200 * time.Millisecond)
	if ok, _ := b.AllowN(1, 5, 1, later); !ok {
		t.Error("no token had refilled after a fifth of a second")
	}
	if ok, _ := b.AllowN(1, 5, 1, later); ok {
		t.Error("two tokens refilled where one was due")
	}
}

// A refused call must not spend the allowance of the retry that follows it.
func TestBurstRefusalDoesNotConsumeAToken(t *testing.T) {
	b := NewBurstLimiters()
	now := time.Now()
	for i := 0; i < BurstDepthFor(2); i++ {
		b.AllowN(7, 2, 1, now)
	}
	// Hammer it while empty.
	for i := 0; i < 50; i++ {
		if ok, _ := b.AllowN(7, 2, 1, now); ok {
			t.Fatal("allowed while the bucket was empty")
		}
	}
	// One token's worth later at 2/s, exactly one call should get through --
	// which it would not if the refusals had been taking tokens.
	later := now.Add(500 * time.Millisecond)
	if ok, _ := b.AllowN(7, 2, 1, later); !ok {
		t.Error("the refusals had eaten the refill")
	}
}

// One key's flood must not throttle another.
func TestBurstIsPerKey(t *testing.T) {
	b := NewBurstLimiters()
	now := time.Now()
	for i := 0; i < BurstDepthFor(5); i++ {
		b.AllowN(1, 5, 1, now)
	}
	if ok, _ := b.AllowN(1, 5, 1, now); ok {
		t.Fatal("key 1 should be empty")
	}
	if ok, _ := b.AllowN(2, 5, 1, now); !ok {
		t.Error("key 2 was throttled by key 1's flood")
	}
}

// A plan change alters the allowance; the key must not keep the rate it
// first arrived with.
func TestBurstFollowsARateChange(t *testing.T) {
	b := NewBurstLimiters()
	now := time.Now()
	for i := 0; i < BurstDepthFor(5); i++ {
		b.AllowN(3, 5, 1, now)
	}
	if ok, _ := b.AllowN(3, 5, 1, now); ok {
		t.Fatal("should be empty at the old rate")
	}
	if ok, _ := b.AllowN(3, 50, 1, now); !ok {
		t.Error("an upgraded key was still held to its old bucket")
	}
}

// A rate of zero or less is how a deployment opts out.
func TestBurstZeroDisables(t *testing.T) {
	b := NewBurstLimiters()
	now := time.Now()
	for i := 0; i < 1000; i++ {
		if ok, _ := b.AllowN(1, 0, 1, now); !ok {
			t.Fatal("a zero rate refused a call")
		}
	}
	if b.Size() != 0 {
		t.Errorf("a disabled limiter kept %d buckets", b.Size())
	}
}

// Idle keys are forgotten, or the map grows for the life of the process.
func TestBurstForgetsIdleKeys(t *testing.T) {
	b := NewBurstLimiters()
	now := time.Now()
	for id := 1; id <= 100; id++ {
		b.AllowN(id, 5, 1, now)
	}
	if b.Size() != 100 {
		t.Fatalf("held %d buckets, want 100", b.Size())
	}

	// One key stays active; the rest go quiet and age out.
	later := now.Add(burstKeyTTL + burstSweepEvery + time.Second)
	b.AllowN(1, 5, 1, later)
	if b.Size() != 1 {
		t.Errorf("held %d buckets after the idle ones expired, want 1", b.Size())
	}
}

// A bucket must not be dropped while it still holds a deficit: that would
// hand a throttled caller a fresh allowance for waiting.
func TestBurstTTLOutlastsARefill(t *testing.T) {
	if burstKeyTTL < time.Second {
		t.Fatalf("burstKeyTTL %v is shorter than a full refill", burstKeyTTL)
	}
}

// The limiter is shared by every request; concurrent use must not race or
// hand out more than the rate allows.
func TestBurstUnderConcurrency(t *testing.T) {
	b := NewBurstLimiters()
	now := time.Now()
	const rate = 10
	depth := BurstDepthFor(rate)
	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed := 0
	for i := 0; i < depth+100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, _ := b.AllowN(42, rate, 1, now); ok {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if allowed != depth {
		t.Errorf("%d calls allowed in one instant, want exactly the depth %d", allowed, depth)
	}
}

// A batch is charged for the lookups it asks for, not for being one request.
// Without that a caller at the allowed request rate drives a hundred times
// the allowed lookup rate.
func TestBurstChargesABatchByItsItems(t *testing.T) {
	b := NewBurstLimiters()
	now := time.Now()

	// A full batch fits, exactly once: the depth is one batch.
	if ok, _ := b.AllowN(1, 5, MaxBatchItems, now); !ok {
		t.Fatal("a full batch did not fit an empty bucket")
	}
	ok, wait := b.AllowN(1, 5, MaxBatchItems, now)
	if ok {
		t.Fatal("a second full batch was allowed in the same instant")
	}
	// 100 tokens at 5/s is 20 seconds, so the wait is real and worth
	// reporting rather than rounding to a second.
	if wait < 19*time.Second || wait > 21*time.Second {
		t.Errorf("wait = %v, want about 20s", wait)
	}

	// A smaller batch gets through sooner, as its cost is smaller.
	if ok, _ := b.AllowN(1, 5, 5, now.Add(2*time.Second)); !ok {
		t.Error("a 5-item batch was refused after 2s of refill at 5/s")
	}
}

// Every plan can submit a full batch: a bucket that could never hold one
// would refuse it however long the caller waited.
func TestBurstDepthHoldsAFullBatch(t *testing.T) {
	for _, perSecond := range []int{1, 5, 10, 25, 50, 500} {
		if depth := BurstDepthFor(perSecond); depth < MaxBatchItems {
			t.Errorf("%d/s gives a depth of %d, smaller than a full batch", perSecond, depth)
		}
		b := NewBurstLimiters()
		if ok, _ := b.AllowN(1, perSecond, MaxBatchItems, time.Now()); !ok {
			t.Errorf("%d/s refused a full batch outright", perSecond)
		}
	}
}

// More than a full batch is refused rather than reserved forever.
func TestBurstRefusesMoreThanADepth(t *testing.T) {
	b := NewBurstLimiters()
	if ok, wait := b.AllowN(1, 5, MaxBatchItems+1, time.Now()); ok || wait <= 0 {
		t.Errorf("an oversized request was allowed (ok=%v wait=%v)", ok, wait)
	}
}
