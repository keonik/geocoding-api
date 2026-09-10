package middleware

import (
	"net/http"
	"os"
	"strconv"
	"time"

	"geocoding-api/handlers"

	"github.com/labstack/echo/v4"
	echomiddleware "github.com/labstack/echo/v4/middleware"
	"golang.org/x/time/rate"
)

// Defaults for the unauthenticated auth endpoints. These are deliberately
// tight: /auth/login runs a bcrypt comparison per attempt, so an attacker
// needs no volume at all to cost the server real CPU, and the same endpoint
// is the credential-stuffing surface. A human signing in touches it a handful
// of times a minute at most.
const (
	defaultAuthRatePerMinute = 12
	defaultAuthBurst         = 5
	// Visitors idle this long are forgotten. Longer than the refill window so
	// a limiter is not discarded while it still holds a deficit.
	authVisitorTTL = 10 * time.Minute
)

// AuthRateLimiter throttles unauthenticated auth attempts per client IP.
//
// This protects /auth/login and /auth/register, which sit outside APIKeyAuth
// and so had no limit of any kind: the monthly quota in APIKeyAuth only ever
// applied to callers who already presented a valid key.
//
// Tunable with AUTH_RATE_PER_MINUTE and AUTH_RATE_BURST. Set
// AUTH_RATE_PER_MINUTE=0 to disable entirely, which is worth having for load
// testing but should never be the production setting.
//
// A NOTE ON WHAT "PER IP" MEANS HERE. This keys on echo's c.RealIP(), which
// reads X-Forwarded-For when present. That header is caller-supplied, so
// unless the server is configured to trust only its real proxy, an attacker
// can rotate it and walk straight past this limiter. See ConfigureIPExtractor:
// wiring that up is what makes this middleware more than decorative.
func AuthRateLimiter() echo.MiddlewareFunc {
	perMinute := envInt("AUTH_RATE_PER_MINUTE", defaultAuthRatePerMinute)
	if perMinute <= 0 {
		// Explicitly disabled: hand back a pass-through rather than a limiter
		// with a zero rate, which would reject everything.
		return func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error { return next(c) }
		}
	}

	burst := envInt("AUTH_RATE_BURST", defaultAuthBurst)
	if burst <= 0 {
		burst = defaultAuthBurst
	}

	store := echomiddleware.NewRateLimiterMemoryStoreWithConfig(
		echomiddleware.RateLimiterMemoryStoreConfig{
			Rate:      rate.Limit(float64(perMinute) / 60.0),
			Burst:     burst,
			ExpiresIn: authVisitorTTL,
		},
	)

	// Retry-After is the wait for a single token to refill, which is the
	// soonest a denied caller can succeed.
	retryAfter := strconv.Itoa(int((time.Minute / time.Duration(perMinute)).Seconds()) + 1)

	return echomiddleware.RateLimiterWithConfig(echomiddleware.RateLimiterConfig{
		Store: store,
		IdentifierExtractor: func(c echo.Context) (string, error) {
			return c.RealIP(), nil
		},
		DenyHandler: func(c echo.Context, identifier string, err error) error {
			c.Response().Header().Set("Retry-After", retryAfter)
			return c.JSON(http.StatusTooManyRequests, handlers.GeocodeResponse{
				Success: false,
				Error:   "Too many authentication attempts. Please wait and try again.",
				Data: map[string]interface{}{
					"retry_after_seconds": retryAfter,
					"limit_per_minute":    perMinute,
				},
			})
		},
		ErrorHandler: func(c echo.Context, err error) error {
			return c.JSON(http.StatusForbidden, handlers.GeocodeResponse{
				Success: false,
				Error:   "Could not identify client for rate limiting",
			})
		},
	})
}

// ConfigureIPExtractor tells echo how to derive the client IP.
//
// This matters for more than the limiter above: RecordUsage stores
// c.RealIP() against every metered call, so a wrong answer here quietly
// corrupts the usage analytics too.
//
// Echo's default trusts X-Forwarded-For from anyone. That is the right
// default for a server behind a proxy it controls and the wrong one for a
// server reachable directly, because the header is just a request header and
// any caller can write whatever they like into it.
//
// This deployment sits behind Cloudflare, which strips inbound
// CF-Connecting-IP and sets its own. Trusting that header is therefore sound
// *only if* the origin cannot be reached except through Cloudflare. Because
// that is a deployment property and not something this code can verify, it is
// opt-in: set TRUST_CLOUDFLARE_IP=true once the origin is confirmed to reject
// non-Cloudflare traffic. Without it, we fall back to echo's default rather
// than pretend to a precision we do not have.
func ConfigureIPExtractor(e *echo.Echo) {
	if os.Getenv("TRUST_CLOUDFLARE_IP") != "true" {
		return
	}

	e.IPExtractor = func(req *http.Request) string {
		if ip := req.Header.Get("CF-Connecting-IP"); ip != "" {
			return ip
		}
		// No CF header: either not a Cloudflare request or a direct hit. Use
		// the socket peer, never XFF, which is exactly the header an attacker
		// bypassing Cloudflare would forge.
		return echo.ExtractIPDirect()(req)
	}
}

func envInt(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return v
}
