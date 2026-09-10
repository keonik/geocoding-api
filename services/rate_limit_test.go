package services

import (
	"testing"
	"time"

	"geocoding-api/models"
)

// TestPlanLimitsMatchAdvertised pins the plan table to the numbers the pricing
// endpoint publishes. It exists because three copies of these values drifted
// apart in production: the free plan was advertised at 3,000 calls/month and
// enforced at 100,000. If someone changes a limit, this test should fail and
// make them change it in the one place that feeds both.
func TestPlanLimitsMatchAdvertised(t *testing.T) {
	tests := []struct {
		planType     string
		monthlyLimit int
		dailyLimit   int
	}{
		{"free", 3000, 500},
		{"starter", 30000, 5000},
		{"pro", 500000, 20000},
		{"enterprise", models.Unlimited, models.Unlimited},
	}

	for _, tt := range tests {
		t.Run(tt.planType, func(t *testing.T) {
			plan := models.PlanFor(tt.planType)
			if plan.MonthlyLimit != tt.monthlyLimit {
				t.Errorf("monthly limit = %d, want %d", plan.MonthlyLimit, tt.monthlyLimit)
			}
			if plan.DailyLimit != tt.dailyLimit {
				t.Errorf("daily limit = %d, want %d", plan.DailyLimit, tt.dailyLimit)
			}
			if plan.Key != tt.planType {
				t.Errorf("Key = %q, want %q", plan.Key, tt.planType)
			}
		})
	}
}

// TestPlanForUnknownFallsBackToFree preserves the behaviour of the SQL CASE
// this replaced, whose ELSE branch was the free monthly limit. An unrecognised
// plan_type must not fail open into an unlimited allowance.
func TestPlanForUnknownFallsBackToFree(t *testing.T) {
	for _, planType := range []string{"", "gold", "FREE", "enterprise "} {
		plan := models.PlanFor(planType)
		if plan.MonthlyLimit != 3000 || plan.DailyLimit != 500 {
			t.Errorf("PlanFor(%q) = %d/%d, want the free plan 3000/500",
				planType, plan.MonthlyLimit, plan.DailyLimit)
		}
	}
}

// TestDailyLimitIsNotAboveMonthly guards the coherence of the pair. A daily cap
// at or above the monthly one is dead configuration -- it can never trip -- and
// that is exactly the state pro was in, with 100,000/day against 500,000/month.
func TestDailyLimitIsNotAboveMonthly(t *testing.T) {
	for key, plan := range models.PlanLimits {
		if plan.MonthlyLimit == models.Unlimited || plan.DailyLimit == models.Unlimited {
			continue
		}
		if plan.DailyLimit >= plan.MonthlyLimit {
			t.Errorf("plan %q: daily limit %d >= monthly limit %d, so the daily cap can never trip",
				key, plan.DailyLimit, plan.MonthlyLimit)
		}
	}
}

func TestExceededScope(t *testing.T) {
	tests := []struct {
		name                     string
		monthlyUsage, monthlyCap int
		dailyUsage, dailyCap     int
		want                     string
	}{
		{"well under both", 10, 3000, 5, 500, ""},
		{"one call short of daily", 499, 3000, 499, 500, ""},
		{"one call short of monthly", 2999, 3000, 10, 500, ""},

		// The bug this whole change exists for: usage far below the monthly cap
		// but at the daily one. Reporting this as "monthly" showed the user
		// 500/3000 next to "limit exceeded".
		{"at daily cap only", 500, 3000, 500, 500, ScopeDaily},
		{"over daily cap only", 700, 3000, 700, 500, ScopeDaily},

		{"at monthly cap only", 3000, 3000, 10, 500, ScopeMonthly},
		{"over monthly cap only", 3200, 3000, 10, 500, ScopeMonthly},

		// Daily wins when both are over, because it clears sooner.
		{"over both", 3000, 3000, 500, 500, ScopeDaily},

		{"unlimited daily, over monthly", 3000, 3000, 99999, models.Unlimited, ScopeMonthly},
		{"unlimited monthly, over daily", 99999, models.Unlimited, 500, 500, ScopeDaily},
		{"unlimited both", 10000000, models.Unlimited, 10000000, models.Unlimited, ""},

		{"zero usage", 0, 3000, 0, 500, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := exceededScope(tt.monthlyUsage, tt.monthlyCap, tt.dailyUsage, tt.dailyCap)
			if got != tt.want {
				t.Errorf("exceededScope(%d/%d monthly, %d/%d daily) = %q, want %q",
					tt.monthlyUsage, tt.monthlyCap, tt.dailyUsage, tt.dailyCap, got, tt.want)
			}
		})
	}
}

// TestRateLimitStatusLimit checks that the reported pair matches the cap that
// tripped, which is the whole point of tracking the scope.
func TestRateLimitStatusLimit(t *testing.T) {
	monthlyReset := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	dailyReset := time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)

	status := &RateLimitStatus{
		MonthlyUsage: 512, MonthlyLimit: 3000,
		DailyUsage: 500, DailyLimit: 500,
		Exceeded:     ScopeDaily,
		MonthlyReset: monthlyReset,
		DailyReset:   dailyReset,
	}

	usage, limit, reset := status.Limit()
	if usage != 500 || limit != 500 {
		t.Errorf("daily rejection reported %d/%d, want 500/500", usage, limit)
	}
	if !reset.Equal(dailyReset) {
		t.Errorf("reset = %v, want the daily boundary %v", reset, dailyReset)
	}

	status.Exceeded = ScopeMonthly
	usage, limit, reset = status.Limit()
	if usage != 512 || limit != 3000 {
		t.Errorf("monthly rejection reported %d/%d, want 512/3000", usage, limit)
	}
	if !reset.Equal(monthlyReset) {
		t.Errorf("reset = %v, want the monthly boundary %v", reset, monthlyReset)
	}
}

func TestRateLimitStatusUnlimited(t *testing.T) {
	tests := []struct {
		name      string
		monthly   int
		daily     int
		unlimited bool
	}{
		{"admin", models.Unlimited, models.Unlimited, true},
		{"free", 3000, 500, false},
		{"monthly only", models.Unlimited, 500, false},
		{"daily only", 3000, models.Unlimited, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := &RateLimitStatus{MonthlyLimit: tt.monthly, DailyLimit: tt.daily}
			if got := status.Unlimited(); got != tt.unlimited {
				t.Errorf("Unlimited() = %v, want %v", got, tt.unlimited)
			}
		})
	}
}
