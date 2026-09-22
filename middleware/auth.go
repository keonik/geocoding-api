package middleware

import (
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"geocoding-api/handlers"
	"geocoding-api/models"
	"geocoding-api/services"

	"github.com/labstack/echo/v4"
)

// BillableUnits reports how many lookups the handler performed, defaulting to
// one. A batch declares its item count via services.BillableUnitsKey, so it is
// billed and rate-limited as that many calls rather than as a single request.
func BillableUnits(c echo.Context) int {
	if n, ok := c.Get(services.BillableUnitsKey).(int); ok && n > 1 {
		return n
	}
	return 1
}

// rateLimitStatusKey is the echo context key under which APIKeyAuth publishes
// the *services.RateLimitStatus it computed, for UsageHeader to reuse.
const rateLimitStatusKey = "rate_limit_status"

// scopeLabel renders a limit scope for the human-readable error string.
func scopeLabel(scope string) string {
	if scope == services.ScopeDaily {
		return "Daily"
	}
	return "Monthly"
}

// APIKeyAuth middleware validates API keys and enforces rate limits
func APIKeyAuth() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Skip authentication for certain endpoints
			path := c.Request().URL.Path
			skipPaths := []string{
				"/docs",
				"/api-docs",
				"/openapi",
				"/swagger",
				"/spec",
				"/api/v1/auth/register",
				"/api/v1/auth/login",
				"/api/v1/auth/plans",
				"/api/v1/health",
			}

			for _, skipPath := range skipPaths {
				if strings.HasPrefix(path, skipPath) {
					return next(c)
				}
			}

			// Extract API key from either X-API-Key or Authorization header
			var apiKey string

			// First, try X-API-Key header
			if xApiKey := c.Request().Header.Get("X-API-Key"); xApiKey != "" {
				apiKey = xApiKey
			} else if authHeader := c.Request().Header.Get("Authorization"); authHeader != "" {
				// Parse Bearer token from Authorization header
				parts := strings.SplitN(authHeader, " ", 2)
				if len(parts) != 2 || parts[0] != "Bearer" {
					return c.JSON(http.StatusUnauthorized, handlers.GeocodeResponse{
						Success: false,
						Error:   "Invalid authorization format. Use 'Authorization: Bearer your-api-key' or 'X-API-Key: your-api-key'",
					})
				}
				apiKey = parts[1]
			} else {
				return c.JSON(http.StatusUnauthorized, handlers.GeocodeResponse{
					Success: false,
					Error:   "API key required. Include 'Authorization: Bearer your-api-key' or 'X-API-Key: your-api-key' header",
				})
			}

			// Start timing for usage recording
			startTime := time.Now()

			// Validate API key
			user, keyRecord, err := services.Auth.ValidateAPIKey(apiKey)
			if err != nil {
				return c.JSON(http.StatusUnauthorized, handlers.GeocodeResponse{
					Success: false,
					Error:   "Invalid API key",
				})
			}

			// The burst guard, before the quota queries below. It is the cheap
			// check -- in memory, no round trip -- so a flood is shed before
			// it costs a database read, which is the point of having it.
			//
			// A rejection here is deliberately not written to usage_records:
			// a row per refusal would turn a flood into exactly the database
			// load this is shedding, and the caller was not served. The
			// metric counts them.
			if perSecond := burstLimitFor(user.PlanType); perSecond > 0 {
				// One token here, where the request's weight is not yet known.
				// A batch charges the rest of its items in the handler, the
				// same way it re-checks the quota against its real size.
				if ok, wait := services.KeyBursts.AllowN(keyRecord.ID, perSecond, 1, time.Now()); !ok {
					return denyBurst(c, perSecond, user.PlanType, wait)
				}
				// Published so the batch handler can charge the rest, and so
				// UsageHeader can tell every caller their rate rather than
				// leaving them to discover it by being refused.
				//
				// A burst 429 returns above this point and so carries no
				// X-API-Usage block: those numbers come from a quota query
				// this path exists to skip. Retry-After and the scope header
				// are on it, and the usage block returns with the next call
				// that is served.
				c.Set(services.BurstLimitKey, perSecond)
			}

			// Check rate limits. The result is stashed on the context below so
			// UsageHeader can build its headers from it instead of asking again.
			status, err := services.Auth.CheckRateLimitStatus(user.ID)
			if err != nil {
				return c.JSON(http.StatusInternalServerError, handlers.GeocodeResponse{
					Success: false,
					Error:   "Failed to check rate limit",
				})
			}
			c.Set(rateLimitStatusKey, status)

			if !status.Within {
				// Record over-limit usage (non-billable)
				overLimitEndpoint := getEndpointName(path)
				method := c.Request().Method
				statusCode := http.StatusTooManyRequests
				responseTime := int(time.Since(startTime).Milliseconds())
				ipAddress := c.RealIP()
				userAgent := c.Request().UserAgent()

				go func() {
					err := services.Auth.RecordUsage(
						user.ID, keyRecord.ID, overLimitEndpoint, method,
						statusCode, responseTime, ipAddress, userAgent, false, 1,
					)
					if err != nil {
						log.Printf("Failed to record over-limit usage: %v", err)
					}
				}()

				// Report the cap that actually tripped. Saying "monthly" for a
				// daily rejection showed the user a usage count well under the
				// limit they were told they had exceeded.
				scopeUsage, scopeLimit, reset := status.Limit()
				scope := status.Exceeded

				// Counted by which period ran out. A spike here is the
				// difference between "the API is down" and "a customer hit
				// their plan limit", and those need different responses.
				RecordRateLimitRejection(scope)

				retryAfter := int(time.Until(reset).Seconds())
				if retryAfter < 1 {
					retryAfter = 1
				}
				c.Response().Header().Set("Retry-After", strconv.Itoa(retryAfter))
				c.Response().Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))
				c.Response().Header().Set("X-RateLimit-Scope", scope)

				return c.JSON(http.StatusTooManyRequests, handlers.GeocodeResponse{
					Success: false,
					Error:   scopeLabel(scope) + " API limit exceeded",
					Data: map[string]interface{}{
						// current_usage and monthly_limit keep their original
						// names and now describe the cap that was hit, so an
						// existing client still reads a coherent pair.
						"current_usage": scopeUsage,
						"monthly_limit": scopeLimit,
						"limit":         scopeLimit,
						"limit_scope":   scope,
						"monthly_usage": status.MonthlyUsage,
						"monthly_cap":   status.MonthlyLimit,
						"daily_usage":   status.DailyUsage,
						"daily_cap":     status.DailyLimit,
						"resets_at":     reset.UTC().Format(time.RFC3339),
						"retry_after":   retryAfter,
						"plan_type":     user.PlanType,
						"upgrade_info":  "Consider upgrading your plan for higher limits",
					},
				})
			}

			// The key's own cap, if it has one. Checked after the owner's plan,
			// because a key cap can only ever be tighter: a key cannot be given
			// more than its owner has. A key with no cap costs one indexed read
			// and is never rejected here.
			keyStatus, err := services.Auth.CheckKeyLimit(keyRecord.ID)
			if err != nil {
				// Enforcement failing closed would take the API down over a
				// feature most keys do not use. The owner's plan limit above
				// still applies, so the caller is not unbounded.
				log.Printf("Failed to check key limit for key %d: %v", keyRecord.ID, err)
			} else if keyStatus.HasCap() {
				// Published for the batch handler, which has to check a batch's
				// size against this key's remaining cap before doing the work.
				c.Set(services.KeyLimitStatusKey, keyStatus)
			}
			if keyStatus != nil && keyStatus.Exceeded != "" {
				scopeUsage, scopeLimit, reset := keyStatus.Limit()
				RecordRateLimitRejection(keyStatus.Exceeded)

				retryAfter := int(time.Until(reset).Seconds())
				if retryAfter < 1 {
					retryAfter = 1
				}
				c.Response().Header().Set("Retry-After", strconv.Itoa(retryAfter))
				c.Response().Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))
				c.Response().Header().Set("X-RateLimit-Scope", keyStatus.Exceeded)

				// Recorded as non-billable, like the plan-limit rejection: the
				// caller did not get what they asked for.
				endpointName, method, ip, ua := getEndpointName(path), c.Request().Method, c.RealIP(), c.Request().UserAgent()
				elapsed := int(time.Since(startTime).Milliseconds())
				go func() {
					if err := services.Auth.RecordUsage(user.ID, keyRecord.ID, endpointName, method,
						http.StatusTooManyRequests, elapsed, ip, ua, false, 1); err != nil {
						log.Printf("Failed to record key-limit rejection: %v", err)
					}
				}()

				return c.JSON(http.StatusTooManyRequests, handlers.GeocodeResponse{
					Success: false,
					// Named as the key's limit rather than the plan's, so the
					// owner looks at the key's settings instead of upgrading a
					// plan that still has room.
					Error: "This API key has reached its own limit",
					Data: map[string]interface{}{
						"limit_scope": keyStatus.Exceeded,
						"usage":       scopeUsage,
						"limit":       scopeLimit,
						"resets_at":   reset.UTC().Format(time.RFC3339),
						"retry_after": retryAfter,
						"key_name":    keyRecord.Name,
						"message":     "Raise or remove this key's cap, or use a different key; your plan itself still has allowance",
					},
				})
			}

			// Check endpoint permissions
			endpoint := getEndpointName(path)
			if !services.Auth.HasPermission(keyRecord, endpoint) {
				return c.JSON(http.StatusForbidden, handlers.GeocodeResponse{
					Success: false,
					Error:   "API key does not have permission for this endpoint",
					Data: map[string]interface{}{
						"endpoint":              endpoint,
						"required_permission":   endpoint,
						"available_permissions": keyRecord.Permissions,
					},
				})
			}

			// A session token collapses a typeahead's keystrokes into the one
			// lookup the person is actually making. Decided before the
			// handler runs so the headers below can say what happened, and
			// so the answer is the same one RecordUsage bills on.
			billable := true
			if token := c.QueryParam("session"); token != "" {
				if !services.SessionEligible(path) {
					return c.JSON(http.StatusBadRequest, handlers.GeocodeResponse{
						Success: false,
						Error:   "Session tokens apply to /addresses/search only",
						Data: map[string]interface{}{
							"endpoint": path,
							"message":  "Drop the session parameter, or send this call to /addresses/search",
						},
					})
				}
				if !services.ValidSessionToken(token) {
					return c.JSON(http.StatusBadRequest, handlers.GeocodeResponse{
						Success: false,
						Error:   "Malformed session token",
						Data: map[string]interface{}{
							"message": "8 to 64 characters of letters, digits, and _.:- -- a UUID is ideal",
						},
					})
				}
				state := services.Sessions.Count(keyRecord.ID, token, time.Now())
				billable = state.Billed
				RecordSessionCall(state.Billed)
				c.Response().Before(func() {
					h := c.Response().Header()
					h.Set("X-Session-Billed", strconv.FormatBool(state.Billed))
					h.Set("X-Session-Calls-Remaining", strconv.Itoa(state.Remaining))
					h.Set("X-Session-Expires-In", strconv.Itoa(int(state.ExpiresIn.Seconds())))
				})
			}

			// Store user and key info in context for handlers
			c.Set("user", user)
			c.Set("api_key", keyRecord)
			c.Set("start_time", startTime)

			// Call next handler
			err = next(c)

			// Capture needed values before goroutine (don't pass context to goroutine)
			responseTime := int(time.Since(startTime).Milliseconds())
			method := c.Request().Method
			statusCode := c.Response().Status
			ipAddress := c.RealIP()
			userAgent := c.Request().UserAgent()

			// How many lookups the handler actually performed. One for every
			// endpoint except batch, which sets it to the number of items --
			// read here rather than in the goroutine, since the context must
			// not be touched once the request has returned.
			units := BillableUnits(c)

			// Record usage after request completes
			go func() {
				err := services.Auth.RecordUsage(
					user.ID, keyRecord.ID, endpoint, method,
					statusCode, responseTime, ipAddress, userAgent, billable, units,
				)
				if err != nil {
					log.Printf("Failed to record usage: %v", err)
				}
			}()

			return err
		}
	}
}

// getEndpointName extracts the endpoint name from the path for categorization
func getEndpointName(path string) string {
	if strings.Contains(path, "/coverage") {
		return "coverage"
	}
	if strings.Contains(path, "/reverse") {
		return "reverse"
	}
	if strings.Contains(path, "/enrich") {
		return "enrich"
	}
	if strings.Contains(path, "/geocode/") {
		return "geocode"
	}
	if strings.Contains(path, "/distance/") {
		return "distance"
	}
	if strings.Contains(path, "/nearby/") {
		return "nearby"
	}
	if strings.Contains(path, "/proximity/") {
		return "proximity"
	}
	if strings.Contains(path, "/search") {
		return "search"
	}
	if strings.Contains(path, "/address/validate") {
		return "addresses"
	}
	if strings.Contains(path, "/addresses") {
		return "addresses"
	}
	if strings.Contains(path, "/counties") {
		return "counties"
	}
	if strings.Contains(path, "/cities") {
		return "cities"
	}
	if strings.Contains(path, "/states") {
		return "states"
	}
	if strings.Contains(path, "/admin/") {
		return "admin"
	}
	return "unknown"
}

// RequireUserAuth middleware for endpoints that need user authentication (not API key)
func RequireUserAuth() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Get Authorization header
			authHeader := c.Request().Header.Get("Authorization")
			if authHeader == "" {
				return c.JSON(http.StatusUnauthorized, handlers.GeocodeResponse{
					Success: false,
					Error:   "Authorization header required",
				})
			}

			// Parse Bearer token
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || parts[0] != "Bearer" {
				return c.JSON(http.StatusUnauthorized, handlers.GeocodeResponse{
					Success: false,
					Error:   "Invalid authorization format. Use 'Bearer <token>'",
				})
			}

			tokenString := parts[1]

			// Validate JWT token
			claims, err := services.Auth.ValidateJWT(tokenString)
			if err != nil {
				return c.JSON(http.StatusUnauthorized, handlers.GeocodeResponse{
					Success: false,
					Error:   "Invalid or expired token",
				})
			}

			// Store user info in context
			c.Set("user_id", claims.UserID)
			c.Set("user_email", claims.Email)
			c.Set("is_admin", claims.IsAdmin)
			c.Set("jwt_claims", claims)

			return next(c)
		}
	}
}

// UsageHeader middleware adds usage info to response headers.
//
// It reads the RateLimitStatus that APIKeyAuth already computed and left on the
// context. It used to call CheckRateLimit a second time for exactly these three
// headers, which repeated the plan lookup and both usage aggregates on every
// single request -- doubling the rate-limit cost of the API for three integers
// that were already in hand.
//
// If the status is absent the headers are simply omitted. That happens only
// when this middleware runs without APIKeyAuth ahead of it, which no route does;
// re-querying as a fallback would quietly reintroduce the cost this removes.
func UsageHeader() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Registered BEFORE next() runs, and deliberately so.
			//
			// echo writes the header block on the handler's first write, which
			// for every handler here is c.JSON. A Header().Set() after next()
			// returns therefore mutates a map that has already been flushed:
			// it is visible to httptest.ResponseRecorder.Header(), which hands
			// back the live map, and invisible to every real client. These
			// headers had never reached the wire.
			//
			// A Before hook runs at WriteHeader time, so the values land while
			// the block can still be changed.
			c.Response().Before(func() {
				h := c.Response().Header()

				// The rate, on every served response. Without it a caller
				// learns their per-second limit only by being refused, which
				// is a poor way to find out what pace to keep.
				if perSecond, ok := c.Get(services.BurstLimitKey).(int); ok && perSecond > 0 {
					h.Set("X-RateLimit-Limit-Second", strconv.Itoa(perSecond))
				}

				status, ok := c.Get(rateLimitStatusKey).(*services.RateLimitStatus)
				if !ok {
					return
				}

				h.Set("X-API-Usage-Current", strconv.Itoa(status.MonthlyUsage))
				h.Set("X-API-Usage-Limit", strconv.Itoa(status.MonthlyLimit))
				h.Set("X-API-Usage-Daily", strconv.Itoa(status.DailyUsage))
				h.Set("X-API-Usage-Daily-Limit", strconv.Itoa(status.DailyLimit))
				if status.PlanType != "" {
					h.Set("X-API-Plan", status.PlanType)
				}
				if !status.Unlimited() {
					h.Set("X-RateLimit-Reset", strconv.FormatInt(status.DailyReset.Unix(), 10))
				}
			})

			return next(c)
		}
	}
}

// isAdminEmail checks if the given email is in the ADMIN_EMAILS environment variable
func isAdminEmail(email string) bool {
	adminEmails := os.Getenv("ADMIN_EMAILS")
	if adminEmails == "" {
		return false
	}

	emails := strings.Split(adminEmails, ",")
	for _, adminEmail := range emails {
		if strings.TrimSpace(adminEmail) == email {
			return true
		}
	}
	return false
}

// RequireAdminAuth middleware ensures user is authenticated via JWT and has admin privileges
func RequireAdminAuth() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			log.Printf("[AdminAuth] Request: %s %s", c.Request().Method, c.Request().URL.Path)

			// Use JWT authentication for admin routes
			authHeader := c.Request().Header.Get("Authorization")
			if authHeader == "" {
				log.Println("[AdminAuth] No Authorization header")
				return c.JSON(http.StatusUnauthorized, handlers.GeocodeResponse{
					Success: false,
					Error:   "Authorization header required",
				})
			}

			// Parse Bearer token
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || parts[0] != "Bearer" {
				log.Println("[AdminAuth] Invalid authorization format")
				return c.JSON(http.StatusUnauthorized, handlers.GeocodeResponse{
					Success: false,
					Error:   "Invalid authorization format. Use 'Bearer <token>'",
				})
			}

			tokenString := parts[1]

			// Validate JWT token
			claims, err := services.Auth.ValidateJWT(tokenString)
			if err != nil {
				log.Printf("[AdminAuth] Invalid token: %v", err)
				return c.JSON(http.StatusUnauthorized, handlers.GeocodeResponse{
					Success: false,
					Error:   "Invalid or expired token",
				})
			}

			log.Printf("[AdminAuth] Token valid for user ID: %d", claims.UserID)

			// Get user from database to check admin status
			user, err := services.Auth.GetUserByID(claims.UserID)
			if err != nil {
				log.Printf("[AdminAuth] User not found: %v", err)
				return c.JSON(http.StatusUnauthorized, handlers.GeocodeResponse{
					Success: false,
					Error:   "User not found",
				})
			}

			// Check if user has admin privileges
			if !user.IsAdmin && !isAdminEmail(user.Email) {
				log.Printf("[AdminAuth] User %s is not admin", user.Email)
				return c.JSON(http.StatusForbidden, handlers.GeocodeResponse{
					Success: false,
					Error:   "Admin privileges required",
				})
			}

			log.Printf("[AdminAuth] Admin access granted for user: %s (ID: %d)", user.Email, user.ID)

			// Store user info in context
			c.Set("user_id", user.ID)
			c.Set("user_email", user.Email)
			c.Set("is_admin", true)
			c.Set("user", user)

			return next(c)
		}
	}
}

// denyBurst answers a caller who is going too fast.
//
// The headers go on before the body is written. Setting them afterwards is
// how this codebase once dropped an entire header block on the wire while the
// tests, which read them off the recorder, stayed green.
func denyBurst(c echo.Context, perSecond int, planType string, wait time.Duration) error {
	// Rounded up: a wait of 1.2s reported as 1 invites a retry that is
	// refused again. A batch charging many tokens at once can wait several
	// seconds, so this is not always the one-second case.
	retryAfter := int(math.Ceil(wait.Seconds()))
	if retryAfter < 1 {
		// Sub-second waits round to one: a Retry-After of 0 invites an
		// immediate retry, which is the behaviour being throttled.
		retryAfter = 1
	}
	RecordRateLimitRejection(services.ScopeBurst)
	c.Response().Header().Set("Retry-After", strconv.Itoa(retryAfter))
	c.Response().Header().Set("X-RateLimit-Scope", services.ScopeBurst)
	c.Response().Header().Set("X-RateLimit-Limit-Second", strconv.Itoa(perSecond))
	// retry_after and limit, not names of this path's own invention: both
	// sibling 429s in this file use them, and a client parsing 429s
	// generically should not need a third case. The scope says what the
	// limit is per.
	return c.JSON(http.StatusTooManyRequests, handlers.GeocodeResponse{
		Success: false,
		Error:   "Too many requests per second for this API key",
		Data: map[string]interface{}{
			"limit":       perSecond,
			"limit_scope": services.ScopeBurst,
			"retry_after": retryAfter,
			"plan_type":   planType,
		},
	})
}

// burstOverride is BURST_PER_SECOND, or -1 when it is unset. Read once:
// AuthRateLimiter resolves its environment at construction for the same
// reason, and os.Getenv takes a process-wide lock that has no business on a
// path every authenticated request walks.
var burstOverride = envInt("BURST_PER_SECOND", -1)

// burstLimitFor is how many calls a second a key on this plan may make.
//
// BURST_PER_SECOND overrides every plan, for a deployment that wants one
// number; 0 disables the guard, which is worth having for load testing and
// should not be the production setting.
func burstLimitFor(planType string) int {
	if burstOverride >= 0 {
		return burstOverride
	}
	return models.PlanFor(planType).BurstPerSecond
}
