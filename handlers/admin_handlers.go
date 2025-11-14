package handlers

import (
	"net/http"
	"strconv"
	"time"

	"geocoding-api/models"
	"geocoding-api/services"

	"github.com/labstack/echo/v4"
)

// AdminStatsResponse contains admin dashboard statistics
type AdminStatsResponse struct {
	TotalUsers  int `json:"total_users"`
	ActiveKeys  int `json:"active_keys"`
	CallsToday  int `json:"calls_today"`
	ZipCodes    int `json:"zip_codes"`
}

// AdminUserResponse contains user info for admin dashboard
type AdminUserResponse struct {
	ID        int       `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Company   string    `json:"company"`
	PlanType  string    `json:"plan_type"`
	IsActive  bool      `json:"is_active"`
	IsAdmin   bool      `json:"is_admin"`
	CreatedAt time.Time `json:"created_at"`
}

// AdminAPIKeyResponse contains API key info for admin dashboard
type AdminAPIKeyResponse struct {
	ID          int       `json:"id"`
	UserEmail   string    `json:"user_email"`
	Name        string    `json:"name"`
	KeyPreview  string    `json:"key_preview"`
	IsActive    bool      `json:"is_active"`
	LastUsedAt  *time.Time `json:"last_used_at"`
	CreatedAt   time.Time `json:"created_at"`
}

// SystemStatusResponse contains system health information
type SystemStatusResponse struct {
	DatabaseConnected bool `json:"database_connected"`
	MigrationsCurrent bool `json:"migrations_current"`
	APIHealth         bool `json:"api_health"`
}

// GetUserStatusHandler returns the current user's status and admin privileges
func GetUserStatusHandler(c echo.Context) error {
	// Get user from API key authentication context
	user, ok := c.Get("user").(*models.User)
	if !ok {
		return c.JSON(http.StatusUnauthorized, GeocodeResponse{
			Success: false,
			Error:   "User authentication required",
		})
	}

	// Check admin status
	isAdmin := services.Auth.IsUserAdmin(user.ID)

	return c.JSON(http.StatusOK, GeocodeResponse{
		Success: true,
		Data: map[string]interface{}{
			"id":        user.ID,
			"email":     user.Email,
			"name":      user.Name,
			"company":   user.Company,
			"is_admin":  isAdmin,
			"plan_type": user.PlanType,
			"is_active": user.IsActive,
		},
	})
}

// GetAdminStatsHandler returns dashboard statistics
func GetAdminStatsHandler(c echo.Context) error {
	// Admin middleware already verified admin access, no need to double-check
	stats, err := services.Auth.GetAdminStats()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, GeocodeResponse{
			Success: false,
			Error:   "Failed to get admin statistics",
		})
	}

	return c.JSON(http.StatusOK, GeocodeResponse{
		Success: true,
		Data:    stats,
	})
}

// GetAllUsersHandler returns all users for admin dashboard
func GetAllUsersHandler(c echo.Context) error {
	users, err := services.Auth.GetAllUsers()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, GeocodeResponse{
			Success: false,
			Error:   "Failed to get users",
		})
	}

	return c.JSON(http.StatusOK, GeocodeResponse{
		Success: true,
		Data:    users,
	})
}

// GetAllAPIKeysHandler returns all API keys for admin dashboard
func GetAllAPIKeysHandler(c echo.Context) error {
	apiKeys, err := services.Auth.GetAllAPIKeys()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, GeocodeResponse{
			Success: false,
			Error:   "Failed to get API keys",
		})
	}

	return c.JSON(http.StatusOK, GeocodeResponse{
		Success: true,
		Data:    apiKeys,
	})
}

// UpdateUserStatusHandler toggles user active status
func UpdateUserStatusHandler(c echo.Context) error {
	// Get admin user from API key context (for audit logging)
	_, ok := c.Get("user").(*models.User)
	if !ok {
		return c.JSON(http.StatusUnauthorized, GeocodeResponse{
			Success: false,
			Error:   "Admin authentication required",
		})
	}

	userID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{
			Success: false,
			Error:   "Invalid user ID",
		})
	}

	var req struct {
		IsActive bool `json:"is_active"`
	}

	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{
			Success: false,
			Error:   "Invalid request body",
		})
	}

	err = services.Auth.UpdateUserStatus(userID, req.IsActive)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, GeocodeResponse{
			Success: false,
			Error:   "Failed to update user status",
		})
	}

	return c.JSON(http.StatusOK, GeocodeResponse{
		Success: true,
		Message: "User status updated successfully",
	})
}

// UpdateUserAdminHandler toggles user admin status
func UpdateUserAdminHandler(c echo.Context) error {
	// Get admin user from API key context
	adminUser, ok := c.Get("user").(*models.User)
	if !ok {
		return c.JSON(http.StatusUnauthorized, GeocodeResponse{
			Success: false,
			Error:   "Admin authentication required",
		})
	}

	userID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{
			Success: false,
			Error:   "Invalid user ID",
		})
	}

	// Prevent user from removing their own admin status
	if userID == adminUser.ID {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{
			Success: false,
			Error:   "Cannot modify your own admin status",
		})
	}

	var req struct {
		IsAdmin bool `json:"is_admin"`
	}

	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{
			Success: false,
			Error:   "Invalid request body",
		})
	}

	err = services.Auth.UpdateUserAdmin(userID, req.IsAdmin)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, GeocodeResponse{
			Success: false,
			Error:   "Failed to update admin status",
		})
	}

	return c.JSON(http.StatusOK, GeocodeResponse{
		Success: true,
		Message: "Admin status updated successfully",
	})
}

// GetSystemStatusHandler returns system health information
func GetSystemStatusHandler(c echo.Context) error {
	status, err := services.Auth.GetSystemStatus()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, GeocodeResponse{
			Success: false,
			Error:   "Failed to get system status",
		})
	}

	return c.JSON(http.StatusOK, GeocodeResponse{
		Success: true,
		Data:    status,
	})
}

// GetUserUsageMetricsHandler returns detailed usage metrics for a specific user
func GetUserUsageMetricsHandler(c echo.Context) error {
	userID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{
			Success: false,
			Error:   "Invalid user ID",
		})
	}

	days := 30
	if daysParam := c.QueryParam("days"); daysParam != "" {
		if d, err := strconv.Atoi(daysParam); err == nil && d > 0 && d <= 365 {
			days = d
		}
	}

	metrics, err := services.Auth.GetUserUsageMetrics(userID, days)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, GeocodeResponse{
			Success: false,
			Error:   "Failed to get user metrics",
		})
	}

	return c.JSON(http.StatusOK, GeocodeResponse{
		Success: true,
		Data:    metrics,
	})
}