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
	ID          int        `json:"id" db:"id"`
	UserID      int        `json:"user_id" db:"user_id"`
	Name        string     `json:"name" db:"name"`               // User-friendly name
	KeyHash     string     `json:"-" db:"key_hash"`              // Hashed version, never return actual key
	KeyPreview  string     `json:"key_preview" db:"key_preview"` // First/last few chars for UI
	IsActive    bool       `json:"is_active" db:"is_active"`
	LastUsedAt  *time.Time `json:"last_used_at" db:"last_used_at"`
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
	ExpiresAt   *time.Time `json:"expires_at" db:"expires_at"`
	Permissions JSONArray  `json:"permissions" db:"permissions"` // ["geocode", "distance", "search"]
}

// UsageRecord represents API usage tracking
type UsageRecord struct {
	ID           int       `json:"id" db:"id"`
	UserID       int       `json:"user_id" db:"user_id"`
	APIKeyID     int       `json:"api_key_id" db:"api_key_id"`
	Endpoint     string    `json:"endpoint" db:"endpoint"` // geocode, distance, search, etc.
	Method       string    `json:"method" db:"method"`     // GET, POST
	StatusCode   int       `json:"status_code" db:"status_code"`
	ResponseTime int       `json:"response_time_ms" db:"response_time_ms"` // milliseconds
	IPAddress    string    `json:"ip_address" db:"ip_address"`
	UserAgent    string    `json:"user_agent" db:"user_agent"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	Billable     bool      `json:"billable" db:"billable"` // false for errors, over-limit calls
}

// Subscription represents user subscription and billing info
type Subscription struct {
	ID                 int       `json:"id" db:"id"`
	UserID             int       `json:"user_id" db:"user_id"`
	PlanType           string    `json:"plan_type" db:"plan_type"`
	Status             string    `json:"status" db:"status"` // active, cancelled, past_due
	CurrentPeriodStart time.Time `json:"current_period_start" db:"current_period_start"`
	CurrentPeriodEnd   time.Time `json:"current_period_end" db:"current_period_end"`
	MonthlyLimit       int       `json:"monthly_limit" db:"monthly_limit"`   // API calls per month
	PricePerCall       float64   `json:"price_per_call" db:"price_per_call"` // in cents
	StripeCustomerID   *string   `json:"stripe_customer_id" db:"stripe_customer_id"`
	StripeSubID        *string   `json:"stripe_subscription_id" db:"stripe_subscription_id"`
	CreatedAt          time.Time `json:"created_at" db:"created_at"`
	UpdatedAt          time.Time `json:"updated_at" db:"updated_at"`
}

// UsageSummary represents aggregated usage statistics
type UsageSummary struct {
	UserID            int            `json:"user_id"`
	Month             string         `json:"month"` // YYYY-MM format
	TotalCalls        int            `json:"total_calls"`
	BillableCalls     int            `json:"billable_calls"`
	TotalCost         float64        `json:"total_cost"` // in dollars
	EndpointBreakdown map[string]int `json:"endpoint_breakdown"`
}

// DailyUsage represents usage statistics for a single day
type DailyUsage struct {
	Date            string `json:"date"` // YYYY-MM-DD format
	TotalCalls      int    `json:"total_calls"`
	BillableCalls   int    `json:"billable_calls"`
	UniqueEndpoints int    `json:"unique_endpoints"`
}

// EndpointUsage represents usage statistics by endpoint
type EndpointUsage struct {
	Endpoint        string  `json:"endpoint"`
	TotalCalls      int     `json:"total_calls"`
	BillableCalls   int     `json:"billable_calls"`
	AvgResponseTime float64 `json:"avg_response_time"` // in milliseconds
	SuccessCount    int     `json:"success_count"`
	ErrorCount      int     `json:"error_count"`
}

// KeyUsage represents usage statistics for a single API key
type KeyUsage struct {
	KeyID           int        `json:"key_id"`
	Name            string     `json:"name"`
	KeyPreview      string     `json:"key_preview"`
	IsActive        bool       `json:"is_active"`
	TotalCalls      int        `json:"total_calls"`
	BillableCalls   int        `json:"billable_calls"`
	AvgResponseTime float64    `json:"avg_response_time"` // in milliseconds
	ErrorCount      int        `json:"error_count"`
	LastCall        *time.Time `json:"last_call"` // null when the key made no calls in range
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

// Plan describes one pricing tier. This is the single source of truth for plan
// limits: the enforcement path (AuthService.CheckRateLimit), the public pricing
// endpoint (GetPlansHandler) and subscription creation all resolve from
// PlanLimits rather than carrying their own copy.
//
// Three copies used to exist and they disagreed. The damage was not
// theoretical: RegisterUser creates a subscription row for every new user using
// the numbers here, subscriptions.is_active defaults to true, and
// CheckRateLimit COALESCEd that row ahead of its own defaults -- so a "free"
// plan advertised at 3,000 calls/month was enforced at the 100,000 this table
// used to claim. Migration 20 repairs the rows already written. Keep this table
// and the enforcement path together; do not reintroduce a second set of numbers.
type Plan struct {
	// Key is the plan_type value stored in users.plan_type and
	// subscriptions.plan_type.
	Key string
	// Name is the human label shown on the pricing endpoint.
	Name string
	// MonthlyLimit and DailyLimit are billable calls per period, or Unlimited.
	// Both are enforced; whichever trips first wins.
	MonthlyLimit int
	DailyLimit   int
	// PricePerCall is in cents. PriceMonthly is in dollars.
	PricePerCall float64
	PriceMonthly float64
	// Features are permission scopes, matched against an API key's permissions
	// by HasPermission. DisplayFeatures is marketing copy for the pricing page
	// -- deliberately separate, they are not the same vocabulary.
	Features        []string
	DisplayFeatures []string
}

// Unlimited is the sentinel MonthlyLimit/DailyLimit value meaning "no cap".
// It is -1 because that is what the API has always returned for enterprise.
const Unlimited = -1

// PlanOrder lists plan keys cheapest first, so callers can render the pricing
// table deterministically instead of ranging over a map.
var PlanOrder = []string{"free", "starter", "pro", "enterprise"}

// PlanLimits holds every plan. Values match what GetPlansHandler advertises;
// that is the public contract and the enforcement path must not quietly differ.
var PlanLimits = map[string]Plan{
	"free": {
		Key:             "free",
		Name:            "Free",
		MonthlyLimit:    3000,
		DailyLimit:      500,
		PricePerCall:    0,
		PriceMonthly:    0,
		Features:        []string{"geocode", "search"},
		DisplayFeatures: []string{"Basic geocoding", "City search", "Community support"},
	},
	"starter": {
		Key:             "starter",
		Name:            "Starter",
		MonthlyLimit:    30000,
		DailyLimit:      5000,
		PricePerCall:    0.001, // $0.001 per call
		PriceMonthly:    10,
		Features:        []string{"geocode", "search", "distance"},
		DisplayFeatures: []string{"All Free features", "Distance calculations", "Email support"},
	},
	"pro": {
		Key:          "pro",
		Name:         "Pro",
		MonthlyLimit: 500000,
		// 20,000/day is what the pricing endpoint has always advertised. The
		// enforcement CASE said 100,000, which let a pro key burn the whole
		// 500,000 monthly allowance in five days and made the daily cap
		// useless as a burst guard. The advertised number wins.
		DailyLimit:      20000,
		PricePerCall:    0.0008,
		PriceMonthly:    80,
		Features:        []string{"geocode", "search", "distance", "bulk"},
		DisplayFeatures: []string{"All Starter features", "Bulk operations", "Priority support", "SLA"},
	},
	"enterprise": {
		Key:             "enterprise",
		Name:            "Enterprise",
		MonthlyLimit:    Unlimited,
		DailyLimit:      Unlimited,
		PricePerCall:    0.0005,
		PriceMonthly:    500,
		Features:        []string{"geocode", "search", "distance", "bulk", "priority"},
		DisplayFeatures: []string{"Unlimited usage", "All Pro features", "Custom integrations", "Dedicated support", "99.9% SLA"},
	},
}

// PlanFor resolves a plan_type to its plan, falling back to free for an
// unrecognised value. The SQL CASE this replaced ended in ELSE 3000 -- the free
// monthly limit -- so an unknown plan_type has always been treated as free.
// That behaviour is preserved deliberately: failing open would hand an
// unlimited allowance to anyone with a typo in their plan_type.
func PlanFor(planType string) Plan {
	if p, ok := PlanLimits[planType]; ok {
		return p
	}
	return PlanLimits["free"]
}
