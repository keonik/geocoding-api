package middleware

import (
	"database/sql"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics for the API. Registered once, at package init, because a duplicate
// registration panics and a lazily-registered collector is a race.
var (
	requestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "geocoding_http_requests_total",
			Help: "Requests by route, method and status class.",
		},
		[]string{"route", "method", "status"},
	)

	requestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "geocoding_http_request_duration_seconds",
			Help: "Request latency by route.",
			// Buckets chosen for what this service actually does. The auth
			// path alone used to cost ~338ms, boundary geometry is tens of
			// milliseconds, and an address search should be single digits --
			// so the interesting range is 1ms to a couple of seconds, with
			// resolution where the decisions get made rather than spread
			// evenly across it.
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
		},
		[]string{"route"},
	)

	rateLimitRejections = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "geocoding_rate_limit_rejections_total",
			Help: "Rate limit rejections by the limit that tripped: monthly, daily, key_monthly, key_daily or burst",
		},
		[]string{"scope"},
	)

	dbOpenConnections = prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "geocoding_db_connections_open",
			Help: "Connections currently open to Postgres.",
		},
		func() float64 { return float64(dbStats().OpenConnections) },
	)

	dbInUseConnections = prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "geocoding_db_connections_in_use",
			Help: "Connections currently executing a query.",
		},
		func() float64 { return float64(dbStats().InUse) },
	)

	// The one that matters under load: a request waiting here is a request the
	// pool could not serve, which looks like slowness with no slow query behind
	// it.
	dbWaitCount = prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "geocoding_db_connections_wait_total",
			Help: "Total times a caller waited for a free connection.",
		},
		func() float64 { return float64(dbStats().WaitCount) },
	)
)

// statsSource is set by the application so this package does not import
// database, which would be a cycle through middleware's other users.
var statsSource func() sql.DBStats

// SetDBStatsSource tells the metrics where to read pool statistics from.
func SetDBStatsSource(f func() sql.DBStats) { statsSource = f }

func dbStats() sql.DBStats {
	if statsSource == nil {
		return sql.DBStats{}
	}
	return statsSource()
}

func init() {
	prometheus.MustRegister(
		requestsTotal, requestDuration, rateLimitRejections,
		dbOpenConnections, dbInUseConnections, dbWaitCount,
	)
}

// RecordRateLimitRejection notes that a request was refused for quota.
func RecordRateLimitRejection(scope string) {
	if scope == "" {
		scope = "unknown"
	}
	rateLimitRejections.WithLabelValues(scope).Inc()
}

// Metrics times every request.
//
// The route label is echo's registered pattern, not the request path. Using the
// path would put one label value per ZIP code, per county, per tile coordinate
// -- an unbounded cardinality explosion that takes the scrape down with it. The
// pattern is bounded by the number of routes.
func Metrics() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()
			err := next(c)

			route := c.Path()
			if route == "" {
				// No route matched, so there is no pattern to attribute this
				// to. A single bucket keeps unmatched paths from becoming
				// labels of their own.
				route = "unmatched"
			}

			status := c.Response().Status
			if err != nil {
				if he, ok := err.(*echo.HTTPError); ok {
					status = he.Code
				}
			}

			requestsTotal.WithLabelValues(route, c.Request().Method, strconv.Itoa(status)).Inc()
			requestDuration.WithLabelValues(route).Observe(time.Since(start).Seconds())

			return err
		}
	}
}
