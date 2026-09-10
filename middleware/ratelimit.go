package middleware

import (
	"log"
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
// by default reads a caller-supplied X-Forwarded-For -- an attacker rotates it
// and gets a fresh bucket per request, walking past the limiter and growing
// the visitor map without bound while doing it. ConfigureIPExtractor replaces
// that default and is applied unconditionally in main.go, which is what makes
// this middleware more than decorative.
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
	retryAfterSeconds := int((time.Minute / time.Duration(perMinute)).Seconds()) + 1
	retryAfterHeader := strconv.Itoa(retryAfterSeconds)

	return echomiddleware.RateLimiterWithConfig(echomiddleware.RateLimiterConfig{
		Store: store,
		IdentifierExtractor: func(c echo.Context) (string, error) {
			return c.RealIP(), nil
		},
		DenyHandler: func(c echo.Context, identifier string, err error) error {
			c.Response().Header().Set("Retry-After", retryAfterHeader)
			return c.JSON(http.StatusTooManyRequests, handlers.GeocodeResponse{
				Success: false,
				Error:   "Too many authentication attempts. Please wait and try again.",
				Data: map[string]interface{}{
					// A number, matching the retry_after that APIKeyAuth's 429
					// carries. It used to be the header string here, so a client
					// doing retry_after_seconds * 1000 got NaN from one 429 and
					// a correct delay from the other.
					"retry_after_seconds": retryAfterSeconds,
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
// Echo's default trusts X-Forwarded-For from anyone, which is wrong here in
// every configuration: XFF is an ordinary request header, so any caller can
// write anything into it, and a rate limiter keyed on it limits nothing.
//
// The replacement prefers CF-Connecting-IP and otherwise uses the socket peer.
// It is on by default because it is never worse than what it replaces:
//
//   - Behind Cloudflare (this deployment), Cloudflare strips any inbound
//     CF-Connecting-IP and sets its own, so the value is trustworthy and the
//     limiter sees real client addresses. Falling back to the socket peer
//     instead would put every user behind one shared bucket and throttle the
//     whole world at 12 logins a minute.
//   - Not behind Cloudflare, the header is simply absent and this is the
//     socket peer, which cannot be forged at all.
//   - Reachable directly around Cloudflare, an attacker can forge
//     CF-Connecting-IP -- but such an attacker can equally forge XFF against
//     echo's default, so nothing is lost relative to before.
//
// TRUST_CLOUDFLARE_IP=false restores echo's default for a deployment that
// genuinely needs XFF, but it reopens the bypass described above.
//
// This also decides the ip_address recorded against every metered call, so the
// same reasoning applies to the usage analytics.
func ConfigureIPExtractor(e *echo.Echo) {
	if os.Getenv("TRUST_CLOUDFLARE_IP") == "false" {
		log.Println("TRUST_CLOUDFLARE_IP=false: using echo's default X-Forwarded-For handling, which a caller can forge")
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
