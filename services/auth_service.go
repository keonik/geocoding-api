package services

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"time"

	"geocoding-api/database"
	"geocoding-api/models"

	"github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

// AuthService handles authentication and API key management
type AuthService struct{}

var Auth = &AuthService{}

// RegisterUser creates a new user account
func (as *AuthService) RegisterUser(email, password, name string, company *string) (*models.User, error) {
	// Check if user already exists
	var exists bool
	err := database.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE email = $1)", email).Scan(&exists)
	if err != nil {
		return nil, fmt.Errorf("failed to check user existence: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("user with email %s already exists", email)
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	// Insert user
	var user models.User
	err = database.DB.QueryRow(`
		INSERT INTO users (email, name, company, password_hash, is_active, plan_type, created_at, updated_at)
		VALUES ($1, $2, $3, $4, true, 'free', NOW(), NOW())
		RETURNING id, email, name, company, is_active, plan_type, created_at, updated_at
	`, email, name, company, string(hashedPassword)).Scan(
		&user.ID, &user.Email, &user.Name, &user.Company, 
		&user.IsActive, &user.PlanType, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	// Create default subscription
	err = as.CreateSubscription(user.ID, "free")
	if err != nil {
		log.Printf("Warning: failed to create subscription for user %d: %v", user.ID, err)
	}

	return &user, nil
}

// AuthenticateUser validates user credentials
func (as *AuthService) AuthenticateUser(email, password string) (*models.User, error) {
	var user models.User
	var passwordHash string

	err := database.DB.QueryRow(`
		SELECT id, email, name, company, password_hash, is_active, plan_type, created_at, updated_at
		FROM users WHERE email = $1 AND is_active = true
	`, email).Scan(
		&user.ID, &user.Email, &user.Name, &user.Company, &passwordHash,
		&user.IsActive, &user.PlanType, &user.CreatedAt, &user.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("invalid email or password")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to authenticate user: %w", err)
	}

	// Check password
	err = bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password))
	if err != nil {
		return nil, fmt.Errorf("invalid email or password")
	}

	return &user, nil
}

// GenerateAPIKey creates a new API key for a user
func (as *AuthService) GenerateAPIKey(userID int, name string, permissions []string) (*models.APIKey, string, error) {
	// Generate random API key
	keyBytes := make([]byte, 32)
	_, err := rand.Read(keyBytes)
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate API key: %w", err)
	}

	// Create key with prefix for easy identification
	apiKey := fmt.Sprintf("gk_%s", hex.EncodeToString(keyBytes))
	
	// Hash the key for storage
	hasher := sha256.New()
	hasher.Write([]byte(apiKey))
	keyHash := hex.EncodeToString(hasher.Sum(nil))

	// Create preview (first 8 + last 4 characters)
	keyPreview := fmt.Sprintf("%s...%s", apiKey[:11], apiKey[len(apiKey)-4:])

	// Insert API key
	var key models.APIKey
	var permissionsArray pq.StringArray
	err = database.DB.QueryRow(`
		INSERT INTO api_keys (user_id, name, key_hash, key_preview, is_active, permissions, created_at)
		VALUES ($1, $2, $3, $4, true, $5, NOW())
		RETURNING id, user_id, name, key_preview, is_active, permissions, created_at
	`, userID, name, keyHash, keyPreview, pq.Array(permissions)).Scan(
		&key.ID, &key.UserID, &key.Name, &key.KeyPreview,
		&key.IsActive, &permissionsArray, &key.CreatedAt,
	)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create API key: %w", err)
	}
	
	// Convert pq.StringArray to JSONArray
	key.Permissions = models.JSONArray(permissionsArray)

	return &key, apiKey, nil
}

// ValidateAPIKey checks if an API key is valid and returns user and key info
func (as *AuthService) ValidateAPIKey(apiKey string) (*models.User, *models.APIKey, error) {
	// Hash the provided key to compare with stored hash
	hasher := sha256.New()
	hasher.Write([]byte(apiKey))
	keyHash := hex.EncodeToString(hasher.Sum(nil))

	// Query for API key and associated user
	var key models.APIKey
	var user models.User
	var permissionsArray pq.StringArray
	err := database.DB.QueryRow(`
		SELECT 
			k.id, k.user_id, k.name, k.key_preview, k.is_active, k.permissions, k.created_at, k.expires_at,
			u.id, u.email, u.name, u.company, u.is_active, u.plan_type, u.created_at, u.updated_at
		FROM api_keys k
		JOIN users u ON k.user_id = u.id
		WHERE k.key_hash = $1 AND k.is_active = true AND u.is_active = true
	`, keyHash).Scan(
		&key.ID, &key.UserID, &key.Name, &key.KeyPreview, &key.IsActive, &permissionsArray, &key.CreatedAt, &key.ExpiresAt,
		&user.ID, &user.Email, &user.Name, &user.Company, &user.IsActive, &user.PlanType, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil, fmt.Errorf("invalid API key")
		}
		return nil, nil, fmt.Errorf("failed to validate API key: %w", err)
	}

	// Convert PostgreSQL array to JSONArray
	key.Permissions = models.JSONArray(permissionsArray)

	// Update last used timestamp
	_, err = database.DB.Exec("UPDATE api_keys SET last_used_at = NOW() WHERE id = $1", key.ID)
	if err != nil {
		// Log error but don't fail validation
		log.Printf("Failed to update last_used_at for API key %d: %v", key.ID, err)
	}

	return &user, &key, nil
}

// CheckRateLimit verifies if user has exceeded their monthly limit
func (as *AuthService) CheckRateLimit(userID int) (bool, int, int, error) {
	// Get user's plan type from users table if no subscription exists
	var monthlyLimit int
	err := database.DB.QueryRow(`
		SELECT COALESCE(s.monthly_limit, 
			CASE 
				WHEN u.plan_type = 'free' THEN 1000
				WHEN u.plan_type = 'starter' THEN 10000
				WHEN u.plan_type = 'pro' THEN 100000
				WHEN u.plan_type = 'enterprise' THEN 1000000
				ELSE 1000
			END
		) as monthly_limit
		FROM users u
		LEFT JOIN subscriptions s ON u.id = s.user_id AND s.is_active = true
		WHERE u.id = $1
	`, userID).Scan(&monthlyLimit)
	if err != nil {
		return false, 0, 0, fmt.Errorf("failed to get user plan: %w", err)
	}

	// Count current month's usage
	var currentUsage int
	err = database.DB.QueryRow(`
		SELECT COUNT(*) FROM usage_records 
		WHERE user_id = $1 AND billable = true 
		AND created_at >= date_trunc('month', CURRENT_DATE)
	`, userID).Scan(&currentUsage)
	if err != nil {
		return false, 0, 0, fmt.Errorf("failed to get usage count: %w", err)
	}

	withinLimit := currentUsage < monthlyLimit
	return withinLimit, currentUsage, monthlyLimit, nil
}

// GetUserAPIKeys retrieves all API keys for a user
func (a *AuthService) GetUserAPIKeys(userID int) ([]models.APIKey, error) {
	var apiKeys []models.APIKey
	
	query := `
		SELECT id, user_id, name, key_preview, permissions, 
		       is_active, last_used_at, created_at, expires_at
		FROM api_keys 
		WHERE user_id = $1 AND is_active = true
		ORDER BY created_at DESC
	`
	
	rows, err := database.DB.Query(query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query API keys: %w", err)
	}
	defer rows.Close()
	
	for rows.Next() {
		var key models.APIKey
		var permissionsJSON pq.StringArray
		
		err := rows.Scan(
			&key.ID, &key.UserID, &key.Name, &key.KeyPreview,
			&permissionsJSON, &key.IsActive, &key.LastUsedAt,
			&key.CreatedAt, &key.ExpiresAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan API key: %w", err)
		}
		
		// Convert pq.StringArray to []string
		key.Permissions = []string(permissionsJSON)
		
		apiKeys = append(apiKeys, key)
	}
	
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating API keys: %w", err)
	}
	
	return apiKeys, nil
}

// DeleteAPIKey soft deletes an API key (marks as inactive)
func (a *AuthService) DeleteAPIKey(userID, keyID int) error {
	// First verify the key belongs to the user
	var exists bool
	err := database.DB.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM api_keys WHERE id = $1 AND user_id = $2 AND is_active = true)",
		keyID, userID,
	).Scan(&exists)
	
	if err != nil {
		return fmt.Errorf("failed to verify API key ownership: %w", err)
	}
	
	if !exists {
		return fmt.Errorf("API key not found or access denied")
	}
	
	// Soft delete by marking as inactive
	_, err = database.DB.Exec(
		"UPDATE api_keys SET is_active = false, updated_at = NOW() WHERE id = $1 AND user_id = $2",
		keyID, userID,
	)
	
	if err != nil {
		return fmt.Errorf("failed to delete API key: %w", err)
	}
	
	return nil
}

// RecordUsage logs an API call for billing and analytics
func (as *AuthService) RecordUsage(userID, apiKeyID int, endpoint, method string, statusCode, responseTime int, ipAddress, userAgent string, billable bool) error {
	log.Printf("Recording usage: UserID=%d, APIKeyID=%d, Endpoint=%s, Method=%s, Billable=%t", 
		userID, apiKeyID, endpoint, method, billable)
	
	_, err := database.DB.Exec(`
		INSERT INTO usage_records (user_id, api_key_id, endpoint, method, status_code, response_time_ms, ip_address, user_agent, billable, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
	`, userID, apiKeyID, endpoint, method, statusCode, responseTime, ipAddress, userAgent, billable)
	
	if err != nil {
		log.Printf("Failed to record usage: %v", err)
	} else {
		log.Printf("Successfully recorded usage for user %d", userID)
	}
	
	return err
}

// IsUserAdmin checks if a user has admin privileges
func (as *AuthService) IsUserAdmin(userID int) bool {
	var isAdmin bool
	err := database.DB.QueryRow("SELECT is_admin FROM users WHERE id = $1", userID).Scan(&isAdmin)
	if err != nil {
		log.Printf("Error checking admin status: %v", err)
		return false
	}
	return isAdmin
}

// GetAdminStats returns statistics for admin dashboard
func (as *AuthService) GetAdminStats() (map[string]interface{}, error) {
	stats := make(map[string]interface{})
	
	// Total users
	var totalUsers int
	err := database.DB.QueryRow("SELECT COUNT(*) FROM users").Scan(&totalUsers)
	if err != nil {
		return nil, err
	}
	stats["total_users"] = totalUsers
	
	// Active API keys
	var activeKeys int
	err = database.DB.QueryRow("SELECT COUNT(*) FROM api_keys WHERE is_active = true").Scan(&activeKeys)
	if err != nil {
		return nil, err
	}
	stats["active_keys"] = activeKeys
	
	// API calls today
	var callsToday int
	err = database.DB.QueryRow(`
		SELECT COUNT(*) FROM usage_records 
		WHERE DATE(created_at) = CURRENT_DATE
	`).Scan(&callsToday)
	if err != nil {
		return nil, err
	}
	stats["calls_today"] = callsToday
	
	// ZIP codes count
	var zipCodes int
	err = database.DB.QueryRow("SELECT COUNT(*) FROM zip_codes").Scan(&zipCodes)
	if err != nil {
		return nil, err
	}
	stats["zip_codes"] = zipCodes
	
	return stats, nil
}

// GetAllUsers returns all users for admin dashboard
func (as *AuthService) GetAllUsers() ([]map[string]interface{}, error) {
	rows, err := database.DB.Query(`
		SELECT id, email, name, company, plan_type, is_active, is_admin, created_at
		FROM users 
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	
	var users []map[string]interface{}
	for rows.Next() {
		var id int
		var email, planType string
		var name, company *string
		var isActive, isAdmin bool
		var createdAt time.Time
		
		err := rows.Scan(&id, &email, &name, &company, &planType, &isActive, &isAdmin, &createdAt)
		if err != nil {
			return nil, err
		}
		
		user := map[string]interface{}{
			"id":         id,
			"email":      email,
			"name":       name,
			"company":    company,
			"plan_type":  planType,
			"is_active":  isActive,
			"is_admin":   isAdmin,
			"created_at": createdAt,
		}
		users = append(users, user)
	}
	
	return users, nil
}

// GetAllAPIKeys returns all API keys for admin dashboard
func (as *AuthService) GetAllAPIKeys() ([]map[string]interface{}, error) {
	rows, err := database.DB.Query(`
		SELECT ak.id, u.email, ak.name, ak.key_preview, ak.is_active, ak.last_used_at, ak.created_at
		FROM api_keys ak
		JOIN users u ON ak.user_id = u.id
		ORDER BY ak.created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	
	var apiKeys []map[string]interface{}
	for rows.Next() {
		var id int
		var userEmail, name, keyPreview string
		var isActive bool
		var lastUsedAt *time.Time
		var createdAt time.Time
		
		err := rows.Scan(&id, &userEmail, &name, &keyPreview, &isActive, &lastUsedAt, &createdAt)
		if err != nil {
			return nil, err
		}
		
		apiKey := map[string]interface{}{
			"id":           id,
			"user_email":   userEmail,
			"name":         name,
			"key_preview":  keyPreview,
			"is_active":    isActive,
			"last_used_at": lastUsedAt,
			"created_at":   createdAt,
		}
		apiKeys = append(apiKeys, apiKey)
	}
	
	return apiKeys, nil
}

// UpdateUserStatus updates a user's active status
func (as *AuthService) UpdateUserStatus(userID int, isActive bool) error {
	_, err := database.DB.Exec(`
		UPDATE users SET is_active = $1, updated_at = CURRENT_TIMESTAMP 
		WHERE id = $2
	`, isActive, userID)
	return err
}

// UpdateUserAdmin updates a user's admin status
func (as *AuthService) UpdateUserAdmin(userID int, isAdmin bool) error {
	_, err := database.DB.Exec(`
		UPDATE users SET is_admin = $1, updated_at = CURRENT_TIMESTAMP 
		WHERE id = $2
	`, isAdmin, userID)
	return err
}

// GetSystemStatus returns system health information
func (as *AuthService) GetSystemStatus() (map[string]interface{}, error) {
	status := make(map[string]interface{})
	
	// Check database connection
	err := database.DB.Ping()
	status["database_connected"] = err == nil
	
	// Check if migrations are current (simplified check)
	var migrationCount int
	err = database.DB.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&migrationCount)
	status["migrations_current"] = err == nil && migrationCount >= 7 // Expected number of migrations
	
	return status, nil
}

// CreateSubscription creates a subscription for a user
func (as *AuthService) CreateSubscription(userID int, planType string) error {
	plan, exists := models.PlanLimits[planType]
	if !exists {
		return fmt.Errorf("invalid plan type: %s", planType)
	}

	_, err := database.DB.Exec(`
		INSERT INTO subscriptions (user_id, plan_type, status, current_period_start, current_period_end, monthly_limit, price_per_call, created_at, updated_at)
		VALUES ($1, $2, 'active', date_trunc('month', CURRENT_DATE), date_trunc('month', CURRENT_DATE) + interval '1 month', $3, $4, NOW(), NOW())
		ON CONFLICT (user_id) DO UPDATE SET
			plan_type = EXCLUDED.plan_type,
			monthly_limit = EXCLUDED.monthly_limit,
			price_per_call = EXCLUDED.price_per_call,
			updated_at = NOW()
	`, userID, planType, plan.MonthlyLimit, plan.PricePerCall)

	return err
}

// GetUsageSummary returns usage statistics for a user
func (as *AuthService) GetUsageSummary(userID int, month string) (*models.UsageSummary, error) {
	// If no month specified, use current month
	if month == "" {
		month = time.Now().Format("2006-01")
	}

	var summary models.UsageSummary
	summary.UserID = userID
	summary.Month = month

	// Get total and billable calls
	err := database.DB.QueryRow(`
		SELECT 
			COUNT(*) as total_calls,
			COUNT(*) FILTER (WHERE billable = true) as billable_calls
		FROM usage_records 
		WHERE user_id = $1 AND to_char(created_at, 'YYYY-MM') = $2
	`, userID, month).Scan(&summary.TotalCalls, &summary.BillableCalls)
	if err != nil {
		return nil, fmt.Errorf("failed to get usage summary: %w", err)
	}

	// Get price per call for cost calculation
	var pricePerCall float64
	err = database.DB.QueryRow(`
		SELECT price_per_call FROM subscriptions WHERE user_id = $1
	`, userID).Scan(&pricePerCall)
	if err != nil {
		pricePerCall = 0 // Default for free plan
	}

	summary.TotalCost = float64(summary.BillableCalls) * pricePerCall / 100 // Convert cents to dollars

	// Get endpoint breakdown
	rows, err := database.DB.Query(`
		SELECT endpoint, COUNT(*) 
		FROM usage_records 
		WHERE user_id = $1 AND to_char(created_at, 'YYYY-MM') = $2
		GROUP BY endpoint
	`, userID, month)
	if err != nil {
		return nil, fmt.Errorf("failed to get endpoint breakdown: %w", err)
	}
	defer rows.Close()

	summary.EndpointBreakdown = make(map[string]int)
	for rows.Next() {
		var endpoint string
		var count int
		err := rows.Scan(&endpoint, &count)
		if err != nil {
			continue
		}
		summary.EndpointBreakdown[endpoint] = count
	}

	return &summary, nil
}

// HasPermission checks if an API key has permission for a specific endpoint
func (as *AuthService) HasPermission(apiKey *models.APIKey, endpoint string) bool {
	// Map endpoints to required permissions
	permissionMap := map[string]string{
		"geocode":   "geocode",
		"search":    "search", 
		"distance":  "distance",
		"nearby":    "distance",
		"proximity": "distance",
		"addresses": "addresses",
		"counties":  "counties",
		"admin":     "admin",
	}

	requiredPermission, exists := permissionMap[endpoint]
	if !exists {
		return false // Unknown endpoint
	}

	// Check if API key has the required permission
	for _, permission := range apiKey.Permissions {
		if permission == requiredPermission || permission == "*" {
			return true
		}
	}

	return false
}