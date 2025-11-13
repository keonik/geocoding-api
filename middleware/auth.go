package middleware

import (
	"log"
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
			// Use JWT authentication for admin routes
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

			// Get user from database to check admin status
			user, err := services.Auth.GetUserByID(claims.UserID)
			if err != nil {
				return c.JSON(http.StatusUnauthorized, handlers.GeocodeResponse{
					Success: false,
					Error:   "User not found",
				})
			}

			// Check if user has admin privileges
			if !user.IsAdmin && !isAdminEmail(user.Email) {
				return c.JSON(http.StatusForbidden, handlers.GeocodeResponse{
					Success: false,
					Error:   "Admin privileges required",
				})
			}

			// Store user info in context
			c.Set("user_id", user.ID)
			c.Set("user_email", user.Email)
			c.Set("is_admin", true)
			c.Set("user", user)

			return next(c)
		}
	}
}