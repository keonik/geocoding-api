package middleware

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"geocoding-api/handlers"
	"geocoding-api/models"
	"geocoding-api/services"

	"github.com/labstack/echo/v4"
)

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

			// Extract API key from Authorization header
			authHeader := c.Request().Header.Get("Authorization")
			if authHeader == "" {
				return c.JSON(http.StatusUnauthorized, handlers.GeocodeResponse{
					Success: false,
					Error:   "API key required. Include 'Authorization: Bearer your-api-key' header",
				})
			}

			// Parse Bearer token
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || parts[0] != "Bearer" {
				return c.JSON(http.StatusUnauthorized, handlers.GeocodeResponse{
					Success: false,
					Error:   "Invalid authorization format. Use 'Authorization: Bearer your-api-key'",
				})
			}

			apiKey := parts[1]

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

			// Check rate limits
			withinLimit, currentUsage, monthlyLimit, err := services.Auth.CheckRateLimit(user.ID)
			if err != nil {
				return c.JSON(http.StatusInternalServerError, handlers.GeocodeResponse{
					Success: false,
					Error:   "Failed to check rate limit",
				})
			}

			if !withinLimit {
				// Record over-limit usage (non-billable)
				go recordUsage(c, user.ID, keyRecord.ID, startTime, false)
				
				return c.JSON(http.StatusTooManyRequests, handlers.GeocodeResponse{
					Success: false,
					Error:   "Monthly API limit exceeded",
					Data: map[string]interface{}{
						"current_usage":  currentUsage,
						"monthly_limit":  monthlyLimit,
						"plan_type":      user.PlanType,
						"upgrade_info":   "Consider upgrading your plan for higher limits",
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

			// Record usage after request completes
			go recordUsage(c, user.ID, keyRecord.ID, startTime, true)

			return err
		}
	}
}

// recordUsage logs the API call for billing and analytics
func recordUsage(c echo.Context, userID, apiKeyID int, startTime time.Time, billable bool) {
	responseTime := int(time.Since(startTime).Milliseconds())
	endpoint := getEndpointName(c.Request().URL.Path)
	method := c.Request().Method
	statusCode := c.Response().Status
	ipAddress := c.RealIP()
	userAgent := c.Request().UserAgent()

	err := services.Auth.RecordUsage(
		userID, apiKeyID, endpoint, method, 
		statusCode, responseTime, ipAddress, userAgent, billable,
	)
	if err != nil {
		// Log error but don't fail the request
		c.Logger().Errorf("Failed to record usage: %v", err)
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
	if strings.Contains(path, "/admin/load-data") {
		return "admin"
	}
	return "unknown"
}

// RequireUserAuth middleware for endpoints that need user authentication (not API key)
func RequireUserAuth() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// In a real implementation, you'd validate JWT tokens or sessions here
			// For now, we'll just check for a user ID header (simplified)
			userIDStr := c.Request().Header.Get("X-User-ID")
			if userIDStr == "" {
				return c.JSON(http.StatusUnauthorized, handlers.GeocodeResponse{
					Success: false,
					Error:   "User authentication required",
				})
			}

			userID, err := strconv.Atoi(userIDStr)
			if err != nil {
				return c.JSON(http.StatusBadRequest, handlers.GeocodeResponse{
					Success: false,
					Error:   "Invalid user ID",
				})
			}

			c.Set("user_id", userID)
			return next(c)
		}
	}
}

// UsageHeader middleware adds usage info to response headers
func UsageHeader() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			err := next(c)

			// Add usage info to headers if user is authenticated
			if user, ok := c.Get("user").(*models.User); ok {
				// Get current usage for the user
				if _, currentUsage, monthlyLimit, err := services.Auth.CheckRateLimit(user.ID); err == nil {
					c.Response().Header().Set("X-API-Usage-Current", strconv.Itoa(currentUsage))
					c.Response().Header().Set("X-API-Usage-Limit", strconv.Itoa(monthlyLimit))
					c.Response().Header().Set("X-API-Plan", user.PlanType)
				}
			}

			return err
		}
	}
}

// RequireAdminAuth middleware ensures user is authenticated and has admin privileges
func RequireAdminAuth() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// First, run the regular user auth
			userAuth := RequireUserAuth()
			if err := userAuth(func(c echo.Context) error { return nil })(c); err != nil {
				return err
			}

			// Check if user is admin
			userIDStr := c.Request().Header.Get("X-User-ID")
			userID, err := strconv.Atoi(userIDStr)
			if err != nil {
				return c.JSON(http.StatusUnauthorized, map[string]interface{}{
					"success": false,
					"error":   "Invalid user authentication",
				})
			}

			if !services.Auth.IsUserAdmin(userID) {
				return c.JSON(http.StatusForbidden, map[string]interface{}{
					"success": false,
					"error":   "Admin privileges required",
				})
			}

			return next(c)
		}
	}
}