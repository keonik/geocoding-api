package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
)

// hit sends one request from the given IP and returns the status code.
func hit(t *testing.T, e *echo.Echo, ip string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	req.RemoteAddr = ip + ":12345"
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec.Code
}

func newLimitedEcho(t *testing.T, perMinute, burst string) *echo.Echo {
	t.Helper()
	t.Setenv("AUTH_RATE_PER_MINUTE", perMinute)
	t.Setenv("AUTH_RATE_BURST", burst)

	e := echo.New()
	g := e.Group("/api/v1/auth")
	g.Use(AuthRateLimiter())
	g.POST("/login", func(c echo.Context) error {
		return c.NoContent(http.StatusOK)
	})
	return e
}

// The burst is what an attacker gets for free before the limiter engages, so
// it is the number that actually matters.
func TestAuthRateLimiterDeniesBeyondBurst(t *testing.T) {
	e := newLimitedEcho(t, "12", "3")

	for i := 1; i <= 3; i++ {
		if code := hit(t, e, "203.0.113.7"); code != http.StatusOK {
			t.Fatalf("request %d within burst: got %d, want 200", i, code)
		}
	}

	// The refill rate is 12/min, so the 4th request inside the same tick has
	// no token waiting for it.
	if code := hit(t, e, "203.0.113.7"); code != http.StatusTooManyRequests {
		t.Fatalf("request past burst: got %d, want 429", code)
	}
}

// A shared limiter across all callers would turn one abusive client into an
// outage for everyone else, which is worse than the problem being solved.
func TestAuthRateLimiterIsPerIP(t *testing.T) {
	e := newLimitedEcho(t, "12", "2")

	for i := 0; i < 3; i++ {
		hit(t, e, "203.0.113.7")
	}
	if code := hit(t, e, "203.0.113.7"); code != http.StatusTooManyRequests {
		t.Fatalf("exhausted IP: got %d, want 429", code)
	}

	if code := hit(t, e, "198.51.100.9"); code != http.StatusOK {
		t.Fatalf("second IP should be unaffected: got %d, want 200", code)
	}
}

func TestAuthRateLimiterDenialCarriesRetryAfter(t *testing.T) {
	e := newLimitedEcho(t, "60", "1")

	hit(t, e, "203.0.113.7")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	req.RemoteAddr = "203.0.113.7:12345"
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("got %d, want 429", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got == "" {
		t.Error("429 must carry Retry-After so a client knows when to come back")
	}
}

// Zero means off. A limiter constructed with a zero rate would instead reject
// every request, which is the opposite of what the setting reads like.
func TestAuthRateLimiterDisabledAtZero(t *testing.T) {
	e := newLimitedEcho(t, "0", "1")

	for i := 0; i < 25; i++ {
		if code := hit(t, e, "203.0.113.7"); code != http.StatusOK {
			t.Fatalf("request %d with limiter disabled: got %d, want 200", i, code)
		}
	}
}

// The limiter keys on RealIP, so whether X-Forwarded-For is trusted decides
// whether the limit can be walked past by writing a header.
func TestConfigureIPExtractorIgnoresForgedXFFByDefault(t *testing.T) {
	t.Setenv("TRUST_CLOUDFLARE_IP", "")

	e := echo.New()
	ConfigureIPExtractor(e)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.7:12345"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")

	c := e.NewContext(req, httptest.NewRecorder())
	if got := c.RealIP(); got != "203.0.113.7" {
		t.Errorf("forged XFF was honored: got %q, want the socket peer 203.0.113.7", got)
	}

	req.Header.Set("CF-Connecting-IP", "5.6.7.8")
	c = e.NewContext(req, httptest.NewRecorder())
	if got := c.RealIP(); got != "5.6.7.8" {
		t.Errorf("CF-Connecting-IP not honored: got %q, want 5.6.7.8", got)
	}
}

// The escape hatch has to actually restore echo's default, since a deployment
// that really does sit behind an XFF-setting proxy it controls needs it.
func TestConfigureIPExtractorCanBeDisabled(t *testing.T) {
	t.Setenv("TRUST_CLOUDFLARE_IP", "false")

	e := echo.New()
	ConfigureIPExtractor(e)

	if e.IPExtractor != nil {
		t.Error("TRUST_CLOUDFLARE_IP=false should leave echo's default extractor in place")
	}
}

// The whole point of the throttle is that an attacker cannot mint a fresh
// bucket per request. Forging XFF must not change which limiter a request
// lands in.
func TestForgedXFFCannotEvadeTheThrottle(t *testing.T) {
	t.Setenv("TRUST_CLOUDFLARE_IP", "")
	t.Setenv("AUTH_RATE_PER_MINUTE", "12")
	t.Setenv("AUTH_RATE_BURST", "2")

	e := echo.New()
	ConfigureIPExtractor(e)
	g := e.Group("/api/v1/auth")
	g.Use(AuthRateLimiter())
	g.POST("/login", func(c echo.Context) error { return c.NoContent(http.StatusOK) })

	send := func(forgedXFF string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
		req.RemoteAddr = "203.0.113.7:12345"
		req.Header.Set("X-Forwarded-For", forgedXFF)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec.Code
	}

	// Same socket peer throughout, a different forged XFF every time.
	send("1.1.1.1")
	send("2.2.2.2")
	if code := send("3.3.3.3"); code != http.StatusTooManyRequests {
		t.Errorf("rotating X-Forwarded-For evaded the throttle: got %d, want 429", code)
	}
}
