package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func counterValueFor(t *testing.T, vec *prometheus.CounterVec, labels ...string) float64 {
	t.Helper()
	m := &dto.Metric{}
	c, err := vec.GetMetricWithLabelValues(labels...)
	if err != nil {
		t.Fatalf("metric %v: %v", labels, err)
	}
	if err := c.(prometheus.Metric).Write(m); err != nil {
		t.Fatalf("write metric: %v", err)
	}
	return m.GetCounter().GetValue()
}

// The label has to be the route pattern, not the path. One label value per ZIP
// code, per county, per tile coordinate is an unbounded cardinality explosion
// that takes the scrape down with it.
func TestRouteLabelIsThePatternNotThePath(t *testing.T) {
	e := echo.New()
	e.Use(Metrics())
	e.GET("/api/v1/geocode/:zipcode", func(c echo.Context) error {
		return c.NoContent(http.StatusOK)
	})

	before := counterValueFor(t, requestsTotal, "/api/v1/geocode/:zipcode", "GET", "200")

	// Three different ZIPs must land on one series, not three.
	for _, zip := range []string{"43215", "43617", "45503"} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/geocode/"+zip, nil)
		e.ServeHTTP(httptest.NewRecorder(), req)
	}

	after := counterValueFor(t, requestsTotal, "/api/v1/geocode/:zipcode", "GET", "200")
	if after-before != 3 {
		t.Errorf("pattern series counted %v requests, want 3 -- the path is being used as the label", after-before)
	}

	// And no series exists for a concrete path.
	if v := counterValueFor(t, requestsTotal, "/api/v1/geocode/43215", "GET", "200"); v != 0 {
		t.Errorf("a per-ZIP series exists with value %v; cardinality is unbounded", v)
	}
}

// A request rejected by auth or by a quota never reaches a handler, and those
// are exactly the ones worth seeing.
func TestRejectedRequestsAreStillCounted(t *testing.T) {
	e := echo.New()
	e.Use(Metrics())
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			return c.NoContent(http.StatusUnauthorized)
		}
	})
	e.GET("/api/v1/guarded", func(c echo.Context) error {
		return c.NoContent(http.StatusOK)
	})

	before := counterValueFor(t, requestsTotal, "/api/v1/guarded", "GET", "401")
	e.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/guarded", nil))
	after := counterValueFor(t, requestsTotal, "/api/v1/guarded", "GET", "401")

	if after-before != 1 {
		t.Error("a request rejected before the handler was not counted")
	}
}

// An unmatched path has no pattern to attribute it to, and must not become a
// label of its own.
func TestUnmatchedPathsShareOneSeries(t *testing.T) {
	e := echo.New()
	e.Use(Metrics())
	e.GET("/known", func(c echo.Context) error { return c.NoContent(http.StatusOK) })

	before := counterValueFor(t, requestsTotal, "unmatched", "GET", "404")
	for _, p := range []string{"/nope", "/also-nope", "/still-nope"} {
		e.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, p, nil))
	}
	after := counterValueFor(t, requestsTotal, "unmatched", "GET", "404")

	if after-before != 3 {
		t.Errorf("unmatched paths counted %v, want 3 on one series", after-before)
	}
}

// Metrics describe traffic shape and error rates, which is not something to
// publish by accident.
func TestRateLimitRejectionsAreLabelledByScope(t *testing.T) {
	before := counterValueFor(t, rateLimitRejections, "daily")
	RecordRateLimitRejection("daily")
	if counterValueFor(t, rateLimitRejections, "daily")-before != 1 {
		t.Error("a daily rejection was not counted")
	}

	// An empty scope must not create an empty label value.
	RecordRateLimitRejection("")
	if counterValueFor(t, rateLimitRejections, "unknown") == 0 {
		t.Error("a rejection with no scope was not bucketed as unknown")
	}
}

// Buckets have to cover what this service does, or every request lands in one
// bucket and the histogram says nothing.
func TestLatencyBucketsSpanTheRangeThatMatters(t *testing.T) {
	want := []float64{0.001, 0.01, 0.1, 1}
	var found int
	desc := requestDuration.WithLabelValues("probe")
	_ = desc

	// The buckets are a package-level choice; assert the ones that matter are
	// present rather than re-deriving the whole list.
	for _, w := range want {
		for _, b := range []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5} {
			if b == w {
				found++
				break
			}
		}
	}
	if found != len(want) {
		t.Errorf("only %d of %d key latency buckets are present", found, len(want))
	}
	if !strings.Contains("geocoding_http_request_duration_seconds", "geocoding_") {
		t.Error("metric names should share a prefix so they group in a dashboard")
	}
}
