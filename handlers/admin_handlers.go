package handlers

import (
	"net/http"
	"strconv"
	"time"

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

// GetAdminStatsHandler returns dashboard statistics
func GetAdminStatsHandler(c echo.Context) error {
	// Verify admin access (middleware should handle this, but double-check)
	userIDStr := c.Request().Header.Get("X-User-ID")
	userID, _ := strconv.Atoi(userIDStr)
	
	if !services.Auth.IsUserAdmin(userID) {
		return c.JSON(http.StatusForbidden, GeocodeResponse{
			Success: false,
			Error:   "Admin access required",
		})
	}

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
	userIDStr := c.Request().Header.Get("X-User-ID")
	userID, _ := strconv.Atoi(userIDStr)
	
	if !services.Auth.IsUserAdmin(userID) {
		return c.JSON(http.StatusForbidden, GeocodeResponse{
			Success: false,
			Error:   "Admin access required",
		})
	}

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
	userIDStr := c.Request().Header.Get("X-User-ID")
	userID, _ := strconv.Atoi(userIDStr)
	
	if !services.Auth.IsUserAdmin(userID) {
		return c.JSON(http.StatusForbidden, GeocodeResponse{
			Success: false,
			Error:   "Admin access required",
		})
	}

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
	adminUserIDStr := c.Request().Header.Get("X-User-ID")
	adminUserID, _ := strconv.Atoi(adminUserIDStr)
	
	if !services.Auth.IsUserAdmin(adminUserID) {
		return c.JSON(http.StatusForbidden, GeocodeResponse{
			Success: false,
			Error:   "Admin access required",
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
	adminUserIDStr := c.Request().Header.Get("X-User-ID")
	adminUserID, _ := strconv.Atoi(adminUserIDStr)
	
	if !services.Auth.IsUserAdmin(adminUserID) {
		return c.JSON(http.StatusForbidden, GeocodeResponse{
			Success: false,
			Error:   "Admin access required",
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
	if userID == adminUserID {
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
	userIDStr := c.Request().Header.Get("X-User-ID")
	userID, _ := strconv.Atoi(userIDStr)
	
	if !services.Auth.IsUserAdmin(userID) {
		return c.JSON(http.StatusForbidden, GeocodeResponse{
			Success: false,
			Error:   "Admin access required",
		})
	}

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