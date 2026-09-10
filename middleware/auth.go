package middleware

import (
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"geocoding-api/handlers"
	"geocoding-api/services"

	"github.com/labstack/echo/v4"
)

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
						statusCode, responseTime, ipAddress, userAgent, false,
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

			// Check endpoint permissions
			endpoint := getEndpointName(path)
			if !services.Auth.HasPermission(keyRecord, endpoint) {
				return c.JSON(http.StatusForbidden, handlers.GeocodeResponse{
					Success: false,
					Error:   "API key does not have permission for this endpoint",
					Data: map[string]interface{}{
						"endpoint":          endpoint,
						"required_permission": endpoint,
						"available_permissions": keyRecord.Permissions,
					},
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

			// Record usage after request completes
			go func() {
				err := services.Auth.RecordUsage(
					user.ID, keyRecord.ID, endpoint, method,
					statusCode, responseTime, ipAddress, userAgent, true,
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
			err := next(c)

			status, ok := c.Get(rateLimitStatusKey).(*services.RateLimitStatus)
			if !ok {
				return err
			}

			c.Response().Header().Set("X-API-Usage-Current", strconv.Itoa(status.MonthlyUsage))
			c.Response().Header().Set("X-API-Usage-Limit", strconv.Itoa(status.MonthlyLimit))
			c.Response().Header().Set("X-API-Usage-Daily", strconv.Itoa(status.DailyUsage))
			c.Response().Header().Set("X-API-Usage-Daily-Limit", strconv.Itoa(status.DailyLimit))
			if status.PlanType != "" {
				c.Response().Header().Set("X-API-Plan", status.PlanType)
			}
			if !status.Unlimited() {
				c.Response().Header().Set("X-RateLimit-Reset", strconv.FormatInt(status.DailyReset.Unix(), 10))
			}

			return err
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