package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
)

func serveMetrics(t *testing.T, authHeader string) int {
	t.Helper()
	e := echo.New()
	e.GET("/metrics", MetricsHandler())

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec.Code
}

// Metrics describe traffic shape, error rates and which endpoints are busy.
// That is not something to publish by accident, so the endpoint does not exist
// until it is deliberately switched on.
func TestMetricsAreOffUntilATokenIsSet(t *testing.T) {
	t.Setenv("METRICS_TOKEN", "")

	// 404 rather than 401: an endpoint that is switched off should not
	// advertise that it exists.
	if code := serveMetrics(t, "Bearer anything"); code != http.StatusNotFound {
		t.Errorf("with no token configured, got %d, want 404", code)
	}
}

func TestMetricsRequireTheToken(t *testing.T) {
	t.Setenv("METRICS_TOKEN", "scrape-me")

	if code := serveMetrics(t, ""); code != http.StatusUnauthorized {
		t.Errorf("with no Authorization header, got %d, want 401", code)
	}
	if code := serveMetrics(t, "Bearer wrong"); code != http.StatusUnauthorized {
		t.Errorf("with the wrong token, got %d, want 401", code)
	}
	// A prefix of the real token must not pass, which a naive comparison on
	// truncated input could allow.
	if code := serveMetrics(t, "Bearer scrape"); code != http.StatusUnauthorized {
		t.Errorf("with a prefix of the token, got %d, want 401", code)
	}
	if code := serveMetrics(t, "Bearer scrape-me"); code != http.StatusOK {
		t.Errorf("with the correct token, got %d, want 200", code)
	}
}
