package services

import (
	"database/sql"
	"os"
	"testing"
	"time"

	"geocoding-api/database"
	"geocoding-api/models"

	_ "github.com/lib/pq"
)

// This file proves the free-tier over-provisioning bug end to end against a real
// Postgres, and proves migration 20 closes it. Skipped unless PROBE_DSN is set.
//
// The bug: RegisterUser creates a subscriptions row for every new user using
// models.PlanLimits, that table claimed free = 100,000/month, is_active defaults
// to true, and CheckRateLimit COALESCEs the subscription row ahead of the plan
// default. So the row won and the advertised 3,000 was never what got enforced.

// setupRateLimitSchema creates just the tables the rate-limit path touches, in a
// dedicated schema so the test cannot collide with anything else in the
// database. Returns a cleanup func.
func setupRateLimitSchema(t *testing.T) func() {
	t.Helper()

	dsn := os.Getenv("PROBE_DSN")
	if dsn == "" {
		t.Skip("PROBE_DSN not set")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("failed to open probe database: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("failed to reach probe database: %v", err)
	}

	// An isolated schema, first on the search_path, so "users" here means this
	// test's users table and nothing else.
	if _, err := db.Exec(`DROP SCHEMA IF EXISTS ratelimit_probe CASCADE`); err != nil {
		t.Fatalf("failed to drop probe schema: %v", err)
	}
	if _, err := db.Exec(`CREATE SCHEMA ratelimit_probe`); err != nil {
		t.Fatalf("failed to create probe schema: %v", err)
	}
	if _, err := db.Exec(`SET search_path TO ratelimit_probe, public`); err != nil {
		t.Fatalf("failed to set search_path: %v", err)
	}

	schema := []string{
		`CREATE TABLE users (
			id SERIAL PRIMARY KEY,
			email VARCHAR(255) UNIQUE NOT NULL,
			is_admin BOOLEAN DEFAULT false,
			plan_type VARCHAR(50) DEFAULT 'free'
		)`,
		`CREATE TABLE subscriptions (
			id SERIAL PRIMARY KEY,
			user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
			plan_type VARCHAR(50) NOT NULL,
			monthly_limit INTEGER NOT NULL,
			is_active BOOLEAN DEFAULT true,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE usage_records (
			id SERIAL PRIMARY KEY,
			user_id INTEGER NOT NULL,
			billable BOOLEAN DEFAULT true,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		// Enforcement reads the counters rather than aggregating
		// usage_records (migration 22). These tests assert on limits, not
		// usage, so the table only has to exist -- an absent row is zero.
		`CREATE TABLE usage_counters (
			user_id INTEGER NOT NULL,
			period_kind VARCHAR(5) NOT NULL,
			period_start DATE NOT NULL,
			count BIGINT NOT NULL DEFAULT 0,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (user_id, period_kind, period_start)
		)`,
	}
	for _, stmt := range schema {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("failed to create probe schema objects: %v", err)
		}
	}

	// The service layer and the migration both reach through database.DB.
	// The pool must be capped at one connection: search_path is per-session, so
	// a second connection would land back in public and not see these tables.
	db.SetMaxOpenConns(1)
	previous := database.DB
	database.DB = db

	return func() {
		db.Exec(`DROP SCHEMA IF EXISTS ratelimit_probe CASCADE`)
		db.Close()
		database.DB = previous
	}
}

func seedUser(t *testing.T, email, planType string, isAdmin bool) int {
	t.Helper()

	var id int
	err := database.DB.QueryRow(
		`INSERT INTO users (email, plan_type, is_admin) VALUES ($1, $2, $3) RETURNING id`,
		email, planType, isAdmin,
	).Scan(&id)
	if err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	return id
}

func seedSubscription(t *testing.T, userID int, planType string, monthlyLimit int) {
	t.Helper()

	_, err := database.DB.Exec(
		`INSERT INTO subscriptions (user_id, plan_type, monthly_limit) VALUES ($1, $2, $3)`,
		userID, planType, monthlyLimit,
	)
	if err != nil {
		t.Fatalf("failed to seed subscription: %v", err)
	}
}

func seedUsage(t *testing.T, userID, billableCalls int) {
	t.Helper()

	_, err := database.DB.Exec(
		`INSERT INTO usage_records (user_id, billable, created_at)
		 SELECT $1, true, NOW() FROM generate_series(1, $2)`,
		userID, billableCalls,
	)
	if err != nil {
		t.Fatalf("failed to seed usage: %v", err)
	}

	syncCounters(t)
}

// syncCounters derives usage_counters from the seeded usage_records.
//
// Enforcement reads the counters now (migration 22), so writing audit rows
// alone no longer moves a limit. Deriving them through the production rebuild
// keeps these tests asserting on real behaviour rather than on numbers the
// fixture wrote by hand -- and means a rebuild that stopped agreeing with the
// records would fail here.
func syncCounters(t *testing.T) {
	t.Helper()
	if err := Auth.RebuildUsageCounters(); err != nil {
		t.Fatalf("failed to rebuild usage counters: %v", err)
	}
}

// TestFreePlanLimitIsRepairedByMigration20 is the regression test for the bug.
func TestFreePlanLimitIsRepairedByMigration20(t *testing.T) {
	cleanup := setupRateLimitSchema(t)
	defer cleanup()

	userID := seedUser(t, "free-with-subscription@example.test", "free", false)

	// Exactly what RegisterUser used to write: the old, wrong free limit.
	seedSubscription(t, userID, "free", 100000)

	before, err := Auth.CheckRateLimitStatus(userID)
	if err != nil {
		t.Fatalf("CheckRateLimitStatus failed: %v", err)
	}
	if before.MonthlyLimit != 100000 {
		t.Fatalf("precondition not reproduced: monthly limit = %d, expected the buggy 100000",
			before.MonthlyLimit)
	}
	t.Logf("before migration 20: free user enforced at %d calls/month (advertised 3000)",
		before.MonthlyLimit)

	if err := database.RepairSubscriptionLimits(); err != nil {
		t.Fatalf("migration 20 failed: %v", err)
	}

	after, err := Auth.CheckRateLimitStatus(userID)
	if err != nil {
		t.Fatalf("CheckRateLimitStatus failed after repair: %v", err)
	}
	if after.MonthlyLimit != 3000 {
		t.Errorf("monthly limit = %d after repair, want 3000", after.MonthlyLimit)
	}
	if after.DailyLimit != 500 {
		t.Errorf("daily limit = %d, want 500", after.DailyLimit)
	}
	t.Logf("after migration 20: free user enforced at %d calls/month, %d/day",
		after.MonthlyLimit, after.DailyLimit)
}

// TestMigration20PreservesCustomLimits is the other half of the contract. The
// repair keys on the exact legacy value precisely so a negotiated limit survives
// -- without that, honouring the subscription override at all would be unsafe.
func TestMigration20PreservesCustomLimits(t *testing.T) {
	cleanup := setupRateLimitSchema(t)
	defer cleanup()

	custom := seedUser(t, "negotiated@example.test", "free", false)
	seedSubscription(t, custom, "free", 250000)

	alreadyRight := seedUser(t, "already-correct@example.test", "free", false)
	seedSubscription(t, alreadyRight, "free", 3000)

	if err := database.RepairSubscriptionLimits(); err != nil {
		t.Fatalf("migration 20 failed: %v", err)
	}

	status, err := Auth.CheckRateLimitStatus(custom)
	if err != nil {
		t.Fatalf("CheckRateLimitStatus failed: %v", err)
	}
	if status.MonthlyLimit != 250000 {
		t.Errorf("negotiated limit = %d, want it left at 250000", status.MonthlyLimit)
	}

	status, err = Auth.CheckRateLimitStatus(alreadyRight)
	if err != nil {
		t.Fatalf("CheckRateLimitStatus failed: %v", err)
	}
	if status.MonthlyLimit != 3000 {
		t.Errorf("already-correct limit = %d, want 3000", status.MonthlyLimit)
	}
}

// TestMigration20RepairsEveryPlan covers the other three tiers, including
// enterprise going from a finite 1,000,000 to genuinely unlimited.
func TestMigration20RepairsEveryPlan(t *testing.T) {
	cleanup := setupRateLimitSchema(t)
	defer cleanup()

	tests := []struct {
		planType string
		legacy   int
		want     int
	}{
		{"free", 100000, 3000},
		{"starter", 10000, 30000},
		{"pro", 100000, 500000},
		{"enterprise", 1000000, models.Unlimited},
	}

	ids := make(map[string]int, len(tests))
	for _, tt := range tests {
		id := seedUser(t, tt.planType+"@example.test", tt.planType, false)
		seedSubscription(t, id, tt.planType, tt.legacy)
		ids[tt.planType] = id
	}

	if err := database.RepairSubscriptionLimits(); err != nil {
		t.Fatalf("migration 20 failed: %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.planType, func(t *testing.T) {
			status, err := Auth.CheckRateLimitStatus(ids[tt.planType])
			if err != nil {
				t.Fatalf("CheckRateLimitStatus failed: %v", err)
			}
			if status.MonthlyLimit != tt.want {
				t.Errorf("monthly limit = %d, want %d", status.MonthlyLimit, tt.want)
			}
		})
	}
}

// TestDailyRejectionReportsDailyScope is the reporting bug: a free user who
// burns the daily cap used to be told they had exceeded a monthly limit, next to
// a usage figure well below it.
func TestDailyRejectionReportsDailyScope(t *testing.T) {
	cleanup := setupRateLimitSchema(t)
	defer cleanup()

	userID := seedUser(t, "daily-burner@example.test", "free", false)
	seedUsage(t, userID, 500) // exactly the free daily cap, far under 3000/month

	status, err := Auth.CheckRateLimitStatus(userID)
	if err != nil {
		t.Fatalf("CheckRateLimitStatus failed: %v", err)
	}

	if status.Within {
		t.Fatal("expected the user to be rate limited at the daily cap")
	}
	if status.Exceeded != ScopeDaily {
		t.Errorf("Exceeded = %q, want %q", status.Exceeded, ScopeDaily)
	}

	usage, limit, reset := status.Limit()
	if usage != 500 || limit != 500 {
		t.Errorf("reported %d/%d, want the daily pair 500/500", usage, limit)
	}
	if reset.IsZero() {
		t.Error("daily reset timestamp is zero; Retry-After would be meaningless")
	}
	if status.MonthlyUsage != 500 || status.MonthlyLimit != 3000 {
		t.Errorf("monthly figures = %d/%d, want 500/3000 carried alongside",
			status.MonthlyUsage, status.MonthlyLimit)
	}
	t.Logf("daily cap tripped: reported %d/%d (%s), resets %s",
		usage, limit, status.Exceeded, reset.UTC().Format("2006-01-02T15:04:05Z"))
}

// TestAdminsAreUnlimited guards the short-circuit that skips the count query.
func TestAdminsAreUnlimited(t *testing.T) {
	cleanup := setupRateLimitSchema(t)
	defer cleanup()

	userID := seedUser(t, "admin@example.test", "free", true)
	seedUsage(t, userID, 5000) // far past every free cap

	status, err := Auth.CheckRateLimitStatus(userID)
	if err != nil {
		t.Fatalf("CheckRateLimitStatus failed: %v", err)
	}
	if !status.Within {
		t.Error("admin was rate limited")
	}
	if !status.Unlimited() {
		t.Errorf("admin limits = %d/%d, want unlimited on both",
			status.MonthlyLimit, status.DailyLimit)
	}
}

// TestUsageCountsAreScopedToTheUser checks the single combined aggregate still
// separates users and periods correctly after collapsing two queries into one.
func TestUsageCountsAreScopedToTheUser(t *testing.T) {
	cleanup := setupRateLimitSchema(t)
	defer cleanup()

	mine := seedUser(t, "mine@example.test", "free", false)
	theirs := seedUser(t, "theirs@example.test", "free", false)

	seedUsage(t, mine, 7)
	seedUsage(t, theirs, 11)

	// A call from earlier this month but not today, and one from last month.
	if _, err := database.DB.Exec(
		`INSERT INTO usage_records (user_id, billable, created_at)
		 VALUES ($1, true, date_trunc('month', CURRENT_DATE)),
		        ($1, true, date_trunc('month', CURRENT_DATE) - interval '1 day')`,
		mine,
	); err != nil {
		t.Fatalf("failed to seed dated usage: %v", err)
	}

	// A non-billable call, which must not count against either period.
	if _, err := database.DB.Exec(
		`INSERT INTO usage_records (user_id, billable, created_at) VALUES ($1, false, NOW())`,
		mine,
	); err != nil {
		t.Fatalf("failed to seed non-billable usage: %v", err)
	}

	// These rows were written straight to the audit log, so the counters have
	// to be derived again before enforcement will see them.
	syncCounters(t)

	status, err := Auth.CheckRateLimitStatus(mine)
	if err != nil {
		t.Fatalf("CheckRateLimitStatus failed: %v", err)
	}

	// On the first of a month, date_trunc('month', CURRENT_DATE) IS today, so
	// the row seeded as "earlier this month" also lands inside today and the
	// daily count is 8 rather than 7. That is correct behaviour, not a bug --
	// but asserting a bare 7 turns this test into a time bomb that fails one
	// day in thirty.
	firstOfMonth := time.Now().Day() == 1
	wantDaily := 7
	if firstOfMonth {
		wantDaily = 8
	}

	if status.DailyUsage != wantDaily {
		t.Errorf("daily usage = %d, want %d (first of month: %t)", status.DailyUsage, wantDaily, firstOfMonth)
	}
	if status.MonthlyUsage != 8 {
		t.Errorf("monthly usage = %d, want 8 (7 today plus 1 earlier this month)", status.MonthlyUsage)
	}
	if status.MonthlyReset.IsZero() || status.DailyReset.IsZero() {
		t.Error("reset boundaries not populated by the combined query")
	}
	if !status.MonthlyReset.After(status.DailyReset) {
		t.Errorf("monthly reset %v should be later than daily reset %v",
			status.MonthlyReset, status.DailyReset)
	}
}
