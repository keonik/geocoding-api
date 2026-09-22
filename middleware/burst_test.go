package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"geocoding-api/services"

	"github.com/labstack/echo/v4"
)

// The bucket holds a second's worth and refills at the rate, so a caller may
// arrive in a burst and then proceeds steadily.
func TestBurstAllowsASecondsWorthThenThrottles(t *testing.T) {
	b := newBurstLimiters()
	now := time.Date(2026, time.September, 22, 12, 0, 0, 0, time.UTC)

	for i := 0; i < 5; i++ {
		if ok, _ := b.allow(1, 5, now); !ok {
			t.Fatalf("call %d of the first second's worth was refused", i+1)
		}
	}
	ok, wait := b.allow(1, 5, now)
	if ok {
		t.Fatal("a sixth call in the same instant was allowed")
	}
	if wait <= 0 || wait > time.Second {
		t.Errorf("wait = %v, want a fraction of a second", wait)
	}

	// A fifth of a second later one token has refilled, and only one.
	later := now.Add(200 * time.Millisecond)
	if ok, _ := b.allow(1, 5, later); !ok {
		t.Error("no token had refilled after a fifth of a second")
	}
	if ok, _ := b.allow(1, 5, later); ok {
		t.Error("two tokens refilled where one was due")
	}
}

// A refused call must not spend the allowance of the retry that follows it.
func TestBurstRefusalDoesNotConsumeAToken(t *testing.T) {
	b := newBurstLimiters()
	now := time.Now()
	for i := 0; i < 2; i++ {
		b.allow(7, 2, now)
	}
	// Hammer it while empty.
	for i := 0; i < 50; i++ {
		if ok, _ := b.allow(7, 2, now); ok {
			t.Fatal("allowed while the bucket was empty")
		}
	}
	// One token's worth later, exactly one call should get through -- which
	// it would not if the refusals had been taking tokens.
	later := now.Add(500 * time.Millisecond)
	if ok, _ := b.allow(7, 2, later); !ok {
		t.Error("the refusals had eaten the refill")
	}
}

// One key's flood must not throttle another.
func TestBurstIsPerKey(t *testing.T) {
	b := newBurstLimiters()
	now := time.Now()
	for i := 0; i < 5; i++ {
		b.allow(1, 5, now)
	}
	if ok, _ := b.allow(1, 5, now); ok {
		t.Fatal("key 1 should be empty")
	}
	if ok, _ := b.allow(2, 5, now); !ok {
		t.Error("key 2 was throttled by key 1's flood")
	}
}

// A plan change alters the allowance; the key must not keep the rate it
// first arrived with.
func TestBurstFollowsARateChange(t *testing.T) {
	b := newBurstLimiters()
	now := time.Now()
	for i := 0; i < 5; i++ {
		b.allow(3, 5, now)
	}
	if ok, _ := b.allow(3, 5, now); ok {
		t.Fatal("should be empty at the old rate")
	}
	if ok, _ := b.allow(3, 50, now); !ok {
		t.Error("an upgraded key was still held to its old bucket")
	}
}

// A rate of zero or less is how a deployment opts out.
func TestBurstZeroDisables(t *testing.T) {
	b := newBurstLimiters()
	now := time.Now()
	for i := 0; i < 1000; i++ {
		if ok, _ := b.allow(1, 0, now); !ok {
			t.Fatal("a zero rate refused a call")
		}
	}
	if b.size() != 0 {
		t.Errorf("a disabled limiter kept %d buckets", b.size())
	}
}

// Idle keys are forgotten, or the map grows for the life of the process.
func TestBurstForgetsIdleKeys(t *testing.T) {
	b := newBurstLimiters()
	now := time.Now()
	for id := 1; id <= 100; id++ {
		b.allow(id, 5, now)
	}
	if b.size() != 100 {
		t.Fatalf("held %d buckets, want 100", b.size())
	}

	// One key stays active; the rest go quiet and age out.
	later := now.Add(burstKeyTTL + burstSweepEvery + time.Second)
	b.allow(1, 5, later)
	if b.size() != 1 {
		t.Errorf("held %d buckets after the idle ones expired, want 1", b.size())
	}
}

// A bucket must not be dropped while it still holds a deficit: that would
// hand a throttled caller a fresh allowance for waiting.
func TestBurstTTLOutlastsARefill(t *testing.T) {
	if burstKeyTTL < time.Second {
		t.Fatalf("burstKeyTTL %v is shorter than a full refill", burstKeyTTL)
	}
}

func TestBurstLimitForPlans(t *testing.T) {
	os.Unsetenv("BURST_PER_SECOND")
	if got := burstLimitFor("free"); got != 5 {
		t.Errorf("free = %d, want 5", got)
	}
	if got := burstLimitFor("enterprise"); got != 50 {
		t.Errorf("enterprise = %d, want 50", got)
	}
	// An unknown plan falls back to free, like every other limit.
	if got := burstLimitFor("bogus"); got != 5 {
		t.Errorf("unknown plan = %d, want the free rate", got)
	}

	t.Setenv("BURST_PER_SECOND", "3")
	if got := burstLimitFor("enterprise"); got != 3 {
		t.Errorf("override = %d, want 3", got)
	}
	t.Setenv("BURST_PER_SECOND", "0")
	if got := burstLimitFor("free"); got != 0 {
		t.Errorf("disabled = %d, want 0", got)
	}
	t.Setenv("BURST_PER_SECOND", "not a number")
	if got := burstLimitFor("free"); got != 5 {
		t.Errorf("a malformed override should leave the plan rate, got %d", got)
	}
}

// The limiter is shared by every request; concurrent use must not race or
// hand out more than the rate allows.
func TestBurstUnderConcurrency(t *testing.T) {
	b := newBurstLimiters()
	now := time.Now()
	const rate = 10
	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed := 0
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, _ := b.allow(42, rate, now); ok {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if allowed != rate {
		t.Errorf("%d calls allowed in one instant, want exactly %d", allowed, rate)
	}
}

// The 429 a throttled caller actually receives, over a real connection:
// headers included, since a header block set after the body is written is
// dropped on the wire and a recorder-based test would not notice.
func TestBurstRejectionReachesTheClient(t *testing.T) {
	e := echo.New()
	e.GET("/api/v1/geocode/:zip", func(c echo.Context) error {
		return denyBurst(c, 25, "pro", 120*time.Millisecond)
	})
	srv := httptest.NewServer(e)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/geocode/43215")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", resp.StatusCode)
	}
	// A sub-second wait rounds up: 0 would invite an instant retry.
	if got := resp.Header.Get("Retry-After"); got != "1" {
		t.Errorf("Retry-After = %q, want \"1\"", got)
	}
	if got := resp.Header.Get("X-RateLimit-Scope"); got != services.ScopeBurst {
		t.Errorf("X-RateLimit-Scope = %q", got)
	}
	if got := resp.Header.Get("X-RateLimit-Limit-Second"); got != "25" {
		t.Errorf("X-RateLimit-Limit-Second = %q, want \"25\"", got)
	}

	var body struct {
		Success bool `json:"success"`
		Data    struct {
			LimitPerSecond    int    `json:"limit_per_second"`
			LimitScope        string `json:"limit_scope"`
			RetryAfterSeconds int    `json:"retry_after_seconds"`
			PlanType          string `json:"plan_type"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Success || body.Data.LimitPerSecond != 25 || body.Data.LimitScope != services.ScopeBurst ||
		body.Data.RetryAfterSeconds != 1 || body.Data.PlanType != "pro" {
		t.Errorf("body = %+v", body.Data)
	}
}
