package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"geocoding-api/services"

	"github.com/labstack/echo/v4"
)

// RegisterRequest represents user registration data
type RegisterRequest struct {
	Email    string  `json:"email" validate:"required,email"`
	Password string  `json:"password" validate:"required,min=8"`
	Name     string  `json:"name" validate:"required"`
	Company  *string `json:"company"`
}

// LoginRequest represents user login data
type LoginRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

// CreateAPIKeyRequest represents API key creation data
type CreateAPIKeyRequest struct {
	Name        string   `json:"name" validate:"required"`
	Permissions []string `json:"permissions" validate:"required"`
}

// RegisterHandler handles user registration
func RegisterHandler(c echo.Context) error {
	var req RegisterRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{
			Success: false,
			Error:   "Invalid request format",
		})
	}

	// Basic validation
	if req.Email == "" || req.Password == "" || req.Name == "" {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{
			Success: false,
			Error:   "Email, password, and name are required",
		})
	}

	if len(req.Password) < 8 {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{
			Success: false,
			Error:   "Password must be at least 8 characters long",
		})
	}

	user, err := services.Auth.RegisterUser(req.Email, req.Password, req.Name, req.Company)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			return c.JSON(http.StatusConflict, GeocodeResponse{
				Success: false,
				Error:   err.Error(),
			})
		}
		return c.JSON(http.StatusInternalServerError, GeocodeResponse{
			Success: false,
			Error:   "Failed to create user account",
		})
	}

	return c.JSON(http.StatusCreated, GeocodeResponse{
		Success: true,
		Data: map[string]interface{}{
			"user": user,
			"message": "Account created successfully. You can now create API keys.",
		},
	})
}

// LoginHandler handles user authentication
func LoginHandler(c echo.Context) error {
	var req LoginRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{
			Success: false,
			Error:   "Invalid request format",
		})
	}

	user, err := services.Auth.AuthenticateUser(req.Email, req.Password)
	if err != nil {
		return c.JSON(http.StatusUnauthorized, GeocodeResponse{
			Success: false,
			Error:   "Invalid email or password",
		})
	}

	return c.JSON(http.StatusOK, GeocodeResponse{
		Success: true,
		Data: map[string]interface{}{
			"user": user,
			"message": "Login successful",
		},
	})
}

// CreateAPIKeyHandler creates a new API key for authenticated users
func CreateAPIKeyHandler(c echo.Context) error {
	// In a real app, you'd get userID from JWT token or session
	userIDStr := c.Request().Header.Get("X-User-ID")
	if userIDStr == "" {
		return c.JSON(http.StatusUnauthorized, GeocodeResponse{
			Success: false,
			Error:   "User authentication required",
		})
	}

	userID, err := strconv.Atoi(userIDStr)
	if err != nil {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{
			Success: false,
			Error:   "Invalid user ID",
		})
	}

	var req CreateAPIKeyRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{
			Success: false,
			Error:   "Invalid request format",
		})
	}

	// Validate permissions
	validPermissions := []string{"geocode", "search", "distance", "nearby", "proximity", "*"}
	for _, perm := range req.Permissions {
		valid := false
		for _, validPerm := range validPermissions {
			if perm == validPerm {
				valid = true
				break
			}
		}
		if !valid {
			return c.JSON(http.StatusBadRequest, GeocodeResponse{
				Success: false,
				Error:   "Invalid permission: " + perm,
			})
		}
	}

	apiKey, keyString, err := services.Auth.GenerateAPIKey(userID, req.Name, req.Permissions)
	if err != nil {
		// Log the actual error for debugging
		c.Logger().Errorf("Failed to create API key: %v", err)
		return c.JSON(http.StatusInternalServerError, GeocodeResponse{
			Success: false,
			Error:   "Failed to create API key: " + err.Error(),
		})
	}

	return c.JSON(http.StatusCreated, GeocodeResponse{
		Success: true,
		Data: map[string]interface{}{
			"api_key":     apiKey,
			"key_string":  keyString,
			"message":     "API key created successfully. Store the key securely - it won't be shown again.",
			"warning":     "This is the only time you'll see the full API key. Store it securely!",
		},
	})
}

// GetUsageHandler returns usage statistics for a user
func GetUsageHandler(c echo.Context) error {
	userIDStr := c.Request().Header.Get("X-User-ID")
	if userIDStr == "" {
		return c.JSON(http.StatusUnauthorized, GeocodeResponse{
			Success: false,
			Error:   "User authentication required",
		})
	}

	userID, err := strconv.Atoi(userIDStr)
	if err != nil {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{
			Success: false,
			Error:   "Invalid user ID",
		})
	}

	month := c.QueryParam("month") // Format: YYYY-MM

	summary, err := services.Auth.GetUsageSummary(userID, month)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, GeocodeResponse{
			Success: false,
			Error:   "Failed to get usage statistics",
		})
	}

	// Also get current rate limit status
	withinLimit, currentUsage, monthlyLimit, err := services.Auth.CheckRateLimit(userID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, GeocodeResponse{
			Success: false,
			Error:   "Failed to check rate limit",
		})
	}

	return c.JSON(http.StatusOK, GeocodeResponse{
		Success: true,
		Data: map[string]interface{}{
			"usage_summary": summary,
			"rate_limit": map[string]interface{}{
				"within_limit":   withinLimit,
				"current_usage":  currentUsage,
				"monthly_limit":  monthlyLimit,
				"remaining":      monthlyLimit - currentUsage,
			},
		},
	})
}

// GetAPIKeysHandler returns all API keys for a user
func GetAPIKeysHandler(c echo.Context) error {
	userIDStr := c.Request().Header.Get("X-User-ID")
	if userIDStr == "" {
		return c.JSON(http.StatusUnauthorized, GeocodeResponse{
			Success: false,
			Error:   "User authentication required",
		})
	}

	userID, err := strconv.Atoi(userIDStr)
	if err != nil {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{
			Success: false,
			Error:   "Invalid user ID",
		})
	}

	apiKeys, err := services.Auth.GetUserAPIKeys(userID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, GeocodeResponse{
			Success: false,
			Error:   "Failed to fetch API keys",
		})
	}

	return c.JSON(http.StatusOK, GeocodeResponse{
		Success: true,
		Data: map[string]interface{}{
			"api_keys": apiKeys,
			"count":    len(apiKeys),
		},
	})
}

// DeleteAPIKeyHandler deletes an API key for a user
func DeleteAPIKeyHandler(c echo.Context) error {
	userIDStr := c.Request().Header.Get("X-User-ID")
	if userIDStr == "" {
		return c.JSON(http.StatusUnauthorized, GeocodeResponse{
			Success: false,
			Error:   "User authentication required",
		})
	}

	userID, err := strconv.Atoi(userIDStr)
	if err != nil {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{
			Success: false,
			Error:   "Invalid user ID",
		})
	}

	keyID := c.Param("id")
	keyIDInt, err := strconv.Atoi(keyID)
	if err != nil {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{
			Success: false,
			Error:   "Invalid API key ID",
		})
	}

	err = services.Auth.DeleteAPIKey(userID, keyIDInt)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return c.JSON(http.StatusNotFound, GeocodeResponse{
				Success: false,
				Error:   "API key not found",
			})
		}
		return c.JSON(http.StatusInternalServerError, GeocodeResponse{
			Success: false,
			Error:   "Failed to delete API key",
		})
	}

	return c.JSON(http.StatusOK, GeocodeResponse{
		Success: true,
		Data: map[string]interface{}{
			"message": "API key deleted successfully",
		},
	})
}

// GetPlansHandler returns available pricing plans
func GetPlansHandler(c echo.Context) error {
	return c.JSON(http.StatusOK, GeocodeResponse{
		Success: true,
		Data: map[string]interface{}{
			"plans": map[string]interface{}{
				"free": map[string]interface{}{
					"name":           "Free",
					"monthly_limit":  1000,
					"price_per_call": 0,
					"price_monthly":  0,
					"features":       []string{"Basic geocoding", "City search", "Community support"},
				},
				"starter": map[string]interface{}{
					"name":           "Starter", 
					"monthly_limit":  10000,
					"price_per_call": 0.001,
					"price_monthly":  10,
					"features":       []string{"All Free features", "Distance calculations", "Email support"},
				},
				"pro": map[string]interface{}{
					"name":           "Pro",
					"monthly_limit":  100000,
					"price_per_call": 0.0008,
					"price_monthly":  80,
					"features":       []string{"All Starter features", "Bulk operations", "Priority support", "SLA"},
				},
				"enterprise": map[string]interface{}{
					"name":           "Enterprise",
					"monthly_limit":  1000000,
					"price_per_call": 0.0005,
					"price_monthly":  500,
					"features":       []string{"All Pro features", "Custom integrations", "Dedicated support", "99.9% SLA"},
				},
			},
		},
	})
}