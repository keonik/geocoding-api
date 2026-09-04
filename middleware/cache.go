package middleware

import (
	"fmt"
	"time"

	"github.com/labstack/echo/v4"
)

// CacheStatic marks a response cacheable by shared caches (a CDN) for d.
//
// Use this ONLY on responses whose body does not vary by caller. The boundary
// endpoints qualify: every key gets byte-identical TIGER geometry for a given
// URL, and that geometry changes about once a year.
//
// The tradeoff is deliberate and worth understanding before copying this
// anywhere else. These routes sit behind APIKeyAuth, but a shared cache keys
// on URL, so once an edge has a copy it can serve that URL to callers who
// present no key at all -- and those hits never reach the origin, so they are
// never metered. That is acceptable here because the payload is public-domain
// census geometry. It would not be acceptable for anything user-scoped.
// Switching "public" to "private" below limits caching to the caller's own
// browser and gives up the origin-CPU saving.
func CacheStatic(d time.Duration) echo.MiddlewareFunc {
	value := fmt.Sprintf("public, max-age=%d, immutable", int(d.Seconds()))
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Set before next() so it lands on the response even if the
			// handler writes and returns early.
			c.Response().Header().Set("Cache-Control", value)
			return next(c)
		}
	}
}
