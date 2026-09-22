package services

import (
	"database/sql"
	"fmt"
	"time"

	"geocoding-api/database"
)

// Scope names for a key's own caps, distinct from the owner's plan limits so a
// caller can tell which one they ran into.
const (
	ScopeKeyDaily   = "key_daily"
	ScopeKeyMonthly = "key_monthly"

	// ScopeBurst is the per-second guard, which is not a quota at all: it
	// refills in a second and costs the caller nothing but a wait, so a
	// client that backs off gets through where a daily cap would have held
	// it until midnight.
	ScopeBurst = "burst"
)

// KeyLimitStatusKey is the echo context key under which APIKeyAuth publishes
// a capped key's status, for handlers that spend more than one unit.
const KeyLimitStatusKey = "key_limit_status"

// Remaining is how many more lookups the key may perform before the tighter of
// its own caps stops it. ok is false when the key has no cap of its own.
func (s *KeyLimitStatus) Remaining() (int, bool) {
	remaining, capped := -1, false
	if s.DailyLimit != nil {
		remaining, capped = *s.DailyLimit-s.DailyUsage, true
	}
	if s.MonthlyLimit != nil {
		m := *s.MonthlyLimit - s.MonthlyUsage
		if !capped || m < remaining {
			remaining = m
		}
		capped = true
	}
	if !capped {
		return 0, false
	}
	if remaining < 0 {
		remaining = 0
	}
	return remaining, true
}

// KeyLimitStatus is where one key stands against its own caps.
type KeyLimitStatus struct {
	MonthlyLimit *int `json:"monthly_limit"`
	DailyLimit   *int `json:"daily_limit"`
	MonthlyUsage int  `json:"monthly_usage"`
	DailyUsage   int  `json:"daily_usage"`

	// Exceeded is "", ScopeKeyDaily or ScopeKeyMonthly.
	Exceeded string `json:"exceeded,omitempty"`

	MonthlyReset time.Time `json:"-"`
	DailyReset   time.Time `json:"-"`
}

// HasCap reports whether this key carries any limit of its own.
func (s *KeyLimitStatus) HasCap() bool {
	return s.MonthlyLimit != nil || s.DailyLimit != nil
}

// Limit returns the cap that tripped and when it resets.
func (s *KeyLimitStatus) Limit() (usage, limit int, reset time.Time) {
	if s.Exceeded == ScopeKeyDaily && s.DailyLimit != nil {
		return s.DailyUsage, *s.DailyLimit, s.DailyReset
	}
	if s.MonthlyLimit != nil {
		return s.MonthlyUsage, *s.MonthlyLimit, s.MonthlyReset
	}
	return 0, 0, time.Time{}
}

// CheckKeyLimit reports whether a key is within its own caps.
//
// A key with no cap of its own returns HasCap() == false and is never
// rejected here -- it draws on its owner's quota exactly as before.
//
// Read in its own query rather than folded into ValidateAPIKey, deliberately.
// ValidateAPIKey runs on every request, so a new column in its SELECT fails
// every request until migration 25 lands, and migrations run asynchronously
// while the server is already serving. That window has taken this API down
// three times already. Here, a missing column or table degrades to "no cap",
// which is precisely today's behaviour -- nothing is less protected than it was.
func (as *AuthService) CheckKeyLimit(keyID int) (*KeyLimitStatus, error) {
	status := &KeyLimitStatus{}

	var monthly, daily sql.NullInt64
	err := database.DB.QueryRow(`
		SELECT monthly_limit, daily_limit FROM api_keys WHERE id = $1
	`, keyID).Scan(&monthly, &daily)
	if err != nil {
		if isUndefinedColumn(err) || isUndefinedTable(err) {
			return status, nil
		}
		if err == sql.ErrNoRows {
			return status, nil
		}
		return nil, fmt.Errorf("failed to read key limits: %w", err)
	}

	if monthly.Valid {
		v := int(monthly.Int64)
		status.MonthlyLimit = &v
	}
	if daily.Valid {
		v := int(daily.Int64)
		status.DailyLimit = &v
	}
	if !status.HasCap() {
		// No cap means no counters to read. This is the common case, and it
		// costs one indexed lookup on a row ValidateAPIKey just touched.
		return status, nil
	}

	err = database.DB.QueryRow(`
		SELECT
			COALESCE(MAX(count) FILTER (WHERE period_kind = 'month'), 0),
			COALESCE(MAX(count) FILTER (WHERE period_kind = 'day'), 0),
			(date_trunc('month', CURRENT_DATE) + interval '1 month')::timestamptz,
			(CURRENT_DATE + interval '1 day')::timestamptz
		FROM api_key_counters
		WHERE api_key_id = $1
		  AND (
			(period_kind = 'month' AND period_start = date_trunc('month', CURRENT_DATE)::date)
			OR (period_kind = 'day' AND period_start = CURRENT_DATE)
		  )
	`, keyID).Scan(&status.MonthlyUsage, &status.DailyUsage, &status.MonthlyReset, &status.DailyReset)
	if err != nil {
		if isUndefinedTable(err) {
			// Caps exist but the counters do not yet. Enforcing against zero
			// would let the key through, which is the pre-migration behaviour;
			// that is the right failure direction for a window this short.
			return status, nil
		}
		return nil, fmt.Errorf("failed to read key usage: %w", err)
	}

	status.Exceeded = keyExceededScope(status)
	return status, nil
}

// keyExceededScope mirrors exceededScope: when both caps are over, the daily one
// is reported, because it clears sooner and is the more useful deadline.
func keyExceededScope(s *KeyLimitStatus) string {
	if s.DailyLimit != nil && s.DailyUsage >= *s.DailyLimit {
		return ScopeKeyDaily
	}
	if s.MonthlyLimit != nil && s.MonthlyUsage >= *s.MonthlyLimit {
		return ScopeKeyMonthly
	}
	return ""
}

// incrementKeyCounters moves a key's own counters. Best effort, like the user
// counters: the audit row is the durable record and the rebuild recovers drift.
func (as *AuthService) incrementKeyCounters(keyID, units int) error {
	_, err := database.DB.Exec(`
		INSERT INTO api_key_counters (api_key_id, period_kind, period_start, count, updated_at)
		VALUES
			($1, 'month', date_trunc('month', CURRENT_DATE)::date, $2, NOW()),
			($1, 'day', CURRENT_DATE, $2, NOW())
		ON CONFLICT (api_key_id, period_kind, period_start) DO UPDATE
		  SET count = api_key_counters.count + EXCLUDED.count, updated_at = NOW()
	`, keyID, units)
	if err != nil && isUndefinedTable(err) {
		return nil
	}
	return err
}

// SetKeyLimits sets or clears a key's own caps. nil clears one.
//
// Scoped to the owner in the WHERE clause rather than checked beforehand, so
// there is no window between the ownership check and the write.
func (as *AuthService) SetKeyLimits(userID, keyID int, monthly, daily *int) error {
	for _, v := range []*int{monthly, daily} {
		if v != nil && *v <= 0 {
			return fmt.Errorf("a limit must be positive; omit it to remove the cap")
		}
	}

	res, err := database.DB.Exec(`
		UPDATE api_keys SET monthly_limit = $1, daily_limit = $2, updated_at = NOW()
		WHERE id = $3 AND user_id = $4
	`, nullableInt(monthly), nullableInt(daily), keyID, userID)
	if err != nil {
		return fmt.Errorf("failed to set key limits: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// Missing and not-yours are deliberately the same answer, so this
		// cannot be used to discover which key ids exist.
		return fmt.Errorf("key not found")
	}
	return nil
}

func nullableInt(v *int) interface{} {
	if v == nil {
		return nil
	}
	return *v
}
