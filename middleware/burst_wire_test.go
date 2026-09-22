package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"geocoding-api/services"

	"github.com/labstack/echo/v4"
)

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

	// The field names the sibling 429s in this file use, so a client
	// parsing 429s generically needs no extra case for this one.
	var body struct {
		Success bool `json:"success"`
		Data    struct {
			Limit      int    `json:"limit"`
			LimitScope string `json:"limit_scope"`
			RetryAfter int    `json:"retry_after"`
			PlanType   string `json:"plan_type"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Success || body.Data.Limit != 25 || body.Data.LimitScope != services.ScopeBurst ||
		body.Data.RetryAfter != 1 || body.Data.PlanType != "pro" {
		t.Errorf("body = %+v", body.Data)
	}
}

// The rate a plan allows, and the override that replaces it.
func TestBurstLimitForPlans(t *testing.T) {
	if got := burstLimitFor("free"); got != 5 {
		t.Errorf("free = %d, want 5", got)
	}
	if got := burstLimitFor("enterprise"); got != 50 {
		t.Errorf("enterprise = %d, want 50", got)
	}
	// An unknown plan falls back to free, like every other limit here.
	if got := burstLimitFor("bogus"); got != 5 {
		t.Errorf("unknown plan = %d, want the free rate", got)
	}

	// BURST_PER_SECOND is resolved once at startup, so the test moves the
	// resolved value rather than the environment it came from.
	restore := burstOverride
	defer func() { burstOverride = restore }()

	burstOverride = 3
	if got := burstLimitFor("enterprise"); got != 3 {
		t.Errorf("override = %d, want 3", got)
	}
	burstOverride = 0
	if got := burstLimitFor("free"); got != 0 {
		t.Errorf("disabled = %d, want 0", got)
	}
}

// A malformed or absent BURST_PER_SECOND leaves the plan rates in charge.
func TestBurstOverrideParsing(t *testing.T) {
	for _, raw := range []string{"", "not a number"} {
		t.Setenv("BURST_PER_SECOND", raw)
		if got := envInt("BURST_PER_SECOND", -1); got != -1 {
			t.Errorf("BURST_PER_SECOND=%q resolved to %d, want the sentinel", raw, got)
		}
	}
	t.Setenv("BURST_PER_SECOND", "0")
	if got := envInt("BURST_PER_SECOND", -1); got != 0 {
		t.Errorf("BURST_PER_SECOND=0 resolved to %d, want 0 (the kill switch)", got)
	}
}
