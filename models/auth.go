package models

import (
	"database/sql/driver"
	"encoding/json"
	"time"

	"github.com/lib/pq"
)

// User represents a registered API user
type User struct {
	ID           int       `json:"id" db:"id"`
	Email        string    `json:"email" db:"email"`
	PasswordHash string    `json:"-" db:"password_hash"` // Hidden from JSON
	Name         string    `json:"name" db:"name"`
	Company      *string   `json:"company,omitempty" db:"company"`
	PlanType     string    `json:"plan_type" db:"plan_type"`
	IsActive     bool      `json:"is_active" db:"is_active"`
	IsAdmin      bool      `json:"is_admin" db:"is_admin"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" db:"updated_at"`
}

// APIKey represents an API key for a user
type APIKey struct {
	ID          int       `json:"id" db:"id"`
	UserID      int       `json:"user_id" db:"user_id"`
	Name        string    `json:"name" db:"name"` // User-friendly name
	KeyHash     string    `json:"-" db:"key_hash"` // Hashed version, never return actual key
	KeyPreview  string    `json:"key_preview" db:"key_preview"` // First/last few chars for UI
	IsActive    bool      `json:"is_active" db:"is_active"`
	LastUsedAt  *time.Time `json:"last_used_at" db:"last_used_at"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	ExpiresAt   *time.Time `json:"expires_at" db:"expires_at"`
	Permissions JSONArray `json:"permissions" db:"permissions"` // ["geocode", "distance", "search"]
}

// UsageRecord represents API usage tracking
type UsageRecord struct {
	ID          int       `json:"id" db:"id"`
	UserID      int       `json:"user_id" db:"user_id"`
	APIKeyID    int       `json:"api_key_id" db:"api_key_id"`
	Endpoint    string    `json:"endpoint" db:"endpoint"` // geocode, distance, search, etc.
	Method      string    `json:"method" db:"method"` // GET, POST
	StatusCode  int       `json:"status_code" db:"status_code"`
	ResponseTime int      `json:"response_time_ms" db:"response_time_ms"` // milliseconds
	IPAddress   string    `json:"ip_address" db:"ip_address"`
	UserAgent   string    `json:"user_agent" db:"user_agent"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	Billable    bool      `json:"billable" db:"billable"` // false for errors, over-limit calls
}

// Subscription represents user subscription and billing info
type Subscription struct {
	ID                int       `json:"id" db:"id"`
	UserID            int       `json:"user_id" db:"user_id"`
	PlanType          string    `json:"plan_type" db:"plan_type"`
	Status            string    `json:"status" db:"status"` // active, cancelled, past_due
	CurrentPeriodStart time.Time `json:"current_period_start" db:"current_period_start"`
	CurrentPeriodEnd   time.Time `json:"current_period_end" db:"current_period_end"`
	MonthlyLimit      int       `json:"monthly_limit" db:"monthly_limit"` // API calls per month
	PricePerCall      float64   `json:"price_per_call" db:"price_per_call"` // in cents
	StripeCustomerID  *string   `json:"stripe_customer_id" db:"stripe_customer_id"`
	StripeSubID       *string   `json:"stripe_subscription_id" db:"stripe_subscription_id"`
	CreatedAt         time.Time `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time `json:"updated_at" db:"updated_at"`
}

// UsageSummary represents aggregated usage statistics
type UsageSummary struct {
	UserID       int     `json:"user_id"`
	Month        string  `json:"month"` // YYYY-MM format
	TotalCalls   int     `json:"total_calls"`
	BillableCalls int    `json:"billable_calls"`
	TotalCost    float64 `json:"total_cost"` // in dollars
	EndpointBreakdown map[string]int `json:"endpoint_breakdown"`
}

// JSONArray for storing array data in database
type JSONArray []string

// Value implements the driver.Valuer interface for JSONArray
func (ja JSONArray) Value() (driver.Value, error) {
	if ja == nil {
		return nil, nil
	}
	return json.Marshal(ja)
}

// Scan implements the sql.Scanner interface for JSONArray
func (ja *JSONArray) Scan(value interface{}) error {
	if value == nil {
		*ja = JSONArray{}
		return nil
	}
	
	// Handle PostgreSQL array format
	if pgArray, ok := value.(pq.StringArray); ok {
		*ja = JSONArray(pgArray)
		return nil
	}
	
	// Handle JSON format (fallback)
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	
	return json.Unmarshal(bytes, ja)
}

// Plan types and limits
var PlanLimits = map[string]struct {
	MonthlyLimit int
	PricePerCall float64 // in cents
	Features     []string
}{
	"free": {
		MonthlyLimit: 1000,
		PricePerCall: 0,
		Features:     []string{"geocode", "search"},
	},
	"starter": {
		MonthlyLimit: 10000,
		PricePerCall: 0.001, // $0.001 per call
		Features:     []string{"geocode", "search", "distance"},
	},
	"pro": {
		MonthlyLimit: 100000,
		PricePerCall: 0.0008,
		Features:     []string{"geocode", "search", "distance", "bulk"},
	},
	"enterprise": {
		MonthlyLimit: 1000000,
		PricePerCall: 0.0005,
		Features:     []string{"geocode", "search", "distance", "bulk", "priority"},
	},
}