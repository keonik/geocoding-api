package handlers

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// MetricsHandler serves Prometheus metrics.
//
// Gated on METRICS_TOKEN rather than the usual API key, because a scraper is
// not a customer: it has no account, runs unattended, and needs a credential
// that does not expire like a JWT. When the variable is unset the endpoint does
// not exist at all -- metrics describe traffic shape, error rates and which
// endpoints are busy, which is not something to publish by accident.
func MetricsHandler() echo.HandlerFunc {
	promHandler := promhttp.Handler()

	return func(c echo.Context) error {
		token := os.Getenv("METRICS_TOKEN")
		if token == "" {
			// 404 rather than 403: an endpoint that is switched off should not
			// advertise that it exists.
			return echo.ErrNotFound
		}

		presented := strings.TrimPrefix(c.Request().Header.Get("Authorization"), "Bearer ")
		// Constant time, so the comparison cannot be used to recover the token
		// one byte at a time.
		if subtle.ConstantTimeCompare([]byte(presented), []byte(token)) != 1 {
			return c.NoContent(http.StatusUnauthorized)
		}

		promHandler.ServeHTTP(c.Response(), c.Request())
		return nil
	}
}
