package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"geocoding-api/services"

	"github.com/labstack/echo/v4"
)

// The recorder-based tests in usage_header_test.go can only prove that the
// header block was correct at flush time. This one proves the values survive
// all the way to a client over a real connection, which is the property that
// was actually broken: headers were being set after c.JSON had already
// committed the response, so every one of them was dropped on the wire while
// the suite stayed green.
func TestUsageHeadersReachAnHTTPClient(t *testing.T) {
	status := &services.RateLimitStatus{
		Within:       true,
		PlanType:     "free",
		MonthlyUsage: 42,
		MonthlyLimit: 3000,
		DailyUsage:   7,
		DailyLimit:   500,
		DailyReset:   time.Unix(1800000000, 0),
	}

	e := echo.New()
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set(rateLimitStatusKey, status)
			return next(c)
		}
	})
	e.Use(UsageHeader())
	// c.JSON, matching what every metered handler actually does -- it is the
	// write that commits the header block.
	e.GET("/api/v1/geocode/:zip", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"zip": c.Param("zip")})
	})

	srv := httptest.NewServer(e)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/geocode/43215")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	want := map[string]string{
		"X-API-Usage-Current":     "42",
		"X-API-Usage-Limit":       "3000",
		"X-API-Usage-Daily":       "7",
		"X-API-Usage-Daily-Limit": "500",
		"X-API-Plan":              "free",
		"X-RateLimit-Reset":       "1800000000",
	}
	for header, expected := range want {
		if got := resp.Header.Get(header); got != expected {
			t.Errorf("%s over the wire = %q, want %q", header, got, expected)
		}
	}
}
