package middleware

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"geocoding-api/models"
	"geocoding-api/services"

	"github.com/labstack/echo/v4"
)

// UsageHeader used to call CheckRateLimit a second time to build these headers,
// which meant it could not be tested without a database. Reading the status
// APIKeyAuth already computed makes it a pure function of the context -- these
// tests are the proof that it no longer queries anything.

func newUsageHeaderContext(status *services.RateLimitStatus) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/geocode/43215", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	if status != nil {
		c.Set(rateLimitStatusKey, status)
	}
	return c, rec
}

func runUsageHeader(c echo.Context) error {
	handler := UsageHeader()(func(c echo.Context) error {
		return c.NoContent(http.StatusOK)
	})
	return handler(c)
}

func TestUsageHeaderReportsBothPeriods(t *testing.T) {
	reset := time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)
	c, rec := newUsageHeaderContext(&services.RateLimitStatus{
		PlanType:     "free",
		MonthlyUsage: 812,
		MonthlyLimit: 3000,
		DailyUsage:   112,
		DailyLimit:   500,
		DailyReset:   reset,
		Within:       true,
	})

	if err := runUsageHeader(c); err != nil {
		t.Fatalf("UsageHeader returned %v", err)
	}

	want := map[string]string{
		"X-API-Usage-Current":     "812",
		"X-API-Usage-Limit":       "3000",
		"X-API-Usage-Daily":       "112",
		"X-API-Usage-Daily-Limit": "500",
		"X-API-Plan":              "free",
		// Derived from reset rather than written as a literal epoch, so the
		// test states the relationship instead of restating the arithmetic.
		"X-RateLimit-Reset": strconv.FormatInt(reset.Unix(), 10),
	}
	for header, expected := range want {
		if got := rec.Header().Get(header); got != expected {
			t.Errorf("%s = %q, want %q", header, got, expected)
		}
	}
}

// An unlimited plan has no meaningful reset instant, so the header is omitted
// rather than carrying a zero time that a client would read as 1970.
func TestUsageHeaderOmitsResetWhenUnlimited(t *testing.T) {
	c, rec := newUsageHeaderContext(&services.RateLimitStatus{
		PlanType:     "enterprise",
		MonthlyLimit: models.Unlimited,
		DailyLimit:   models.Unlimited,
		Within:       true,
	})

	if err := runUsageHeader(c); err != nil {
		t.Fatalf("UsageHeader returned %v", err)
	}

	if got := rec.Header().Get("X-RateLimit-Reset"); got != "" {
		t.Errorf("X-RateLimit-Reset = %q, want it absent for an unlimited plan", got)
	}
	if got := rec.Header().Get("X-API-Usage-Limit"); got != "-1" {
		t.Errorf("X-API-Usage-Limit = %q, want -1", got)
	}
}

// Without APIKeyAuth ahead of it there is nothing on the context. The middleware
// must pass the request through untouched rather than falling back to a query.
func TestUsageHeaderWithoutStatusIsInert(t *testing.T) {
	c, rec := newUsageHeaderContext(nil)

	if err := runUsageHeader(c); err != nil {
		t.Fatalf("UsageHeader returned %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	for _, header := range []string{"X-API-Usage-Current", "X-API-Usage-Limit", "X-API-Plan"} {
		if got := rec.Header().Get(header); got != "" {
			t.Errorf("%s = %q, want it absent", header, got)
		}
	}
}

// The handler's error must reach the caller; the headers are a side effect, not
// a reason to swallow it.
func TestUsageHeaderPropagatesHandlerError(t *testing.T) {
	c, _ := newUsageHeaderContext(&services.RateLimitStatus{PlanType: "free"})

	want := echo.NewHTTPError(http.StatusTeapot, "brewing")
	handler := UsageHeader()(func(c echo.Context) error {
		return want
	})

	if got := handler(c); got != want {
		t.Errorf("error = %v, want it propagated unchanged", got)
	}
}

func TestScopeLabel(t *testing.T) {
	tests := []struct {
		scope string
		want  string
	}{
		{services.ScopeDaily, "Daily"},
		{services.ScopeMonthly, "Monthly"},
		{"", "Monthly"},
	}

	for _, tt := range tests {
		if got := scopeLabel(tt.scope); got != tt.want {
			t.Errorf("scopeLabel(%q) = %q, want %q", tt.scope, got, tt.want)
		}
	}
}
