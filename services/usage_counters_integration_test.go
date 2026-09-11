package services

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"geocoding-api/database"

	_ "github.com/lib/pq"
)

const counterSchema = "usage_counter_probe"

func setupCounterDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := os.Getenv("PROBE_DSN")
	if dsn == "" {
		t.Skip("PROBE_DSN not set")
	}

	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// Closed in t.Cleanup, not deferred: a defer here runs when this function
	// returns, which is before any registered cleanup, so the cleanup would
	// find the handle already closed and silently fail to drop the schema.
	if err := admin.Ping(); err != nil {
		admin.Close()
		t.Skipf("probe database unreachable: %v", err)
	}

	for _, stmt := range []string{
		fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", counterSchema),
		fmt.Sprintf("CREATE SCHEMA %s", counterSchema),
	} {
		if _, err := admin.Exec(stmt); err != nil {
			t.Fatalf("setup failed on %.60q: %v", stmt, err)
		}
	}

	// search_path travels in the connection string, so every connection the
	// pool opens gets it and nothing outside this test is affected.
	//
	// The obvious alternative, ALTER ROLE ... SET search_path, is a trap: it
	// is a persistent cluster-wide setting on the role, so it changes every
	// future connection in every database -- the real application included --
	// and a t.Fatalf before the reset runs leaves it that way for good.
	// Pinning the pool to one connection is the other way, but the
	// concurrency test below needs several.
	scopedDSN, err := withSearchPathOption(dsn, counterSchema)
	if err != nil {
		t.Fatalf("build scoped dsn: %v", err)
	}

	db, err := sql.Open("postgres", scopedDSN)
	if err != nil {
		t.Fatalf("open scoped: %v", err)
	}
	// Bounded so the fan-out below cannot exhaust the server's connection
	// slots. A "too many clients" failure inside RecordUsage is only logged,
	// so it would surface as a bogus "increments were lost" failure pointing
	// at a concurrency bug that is not there.
	db.SetMaxOpenConns(10)

	schema := []string{
		`CREATE TABLE users (
			id SERIAL PRIMARY KEY,
			email VARCHAR(255) NOT NULL UNIQUE,
			password_hash VARCHAR(255) NOT NULL,
			plan_type VARCHAR(50) DEFAULT 'free',
			is_active BOOLEAN DEFAULT true,
			is_admin BOOLEAN DEFAULT false,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			name VARCHAR(255), company VARCHAR(255)
		)`,
		`CREATE TABLE api_keys (
			id SERIAL PRIMARY KEY,
			user_id INTEGER REFERENCES users(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE subscriptions (
			id SERIAL PRIMARY KEY,
			user_id INTEGER REFERENCES users(id) ON DELETE CASCADE UNIQUE,
			plan_type VARCHAR(50) NOT NULL,
			monthly_limit INTEGER,
			is_active BOOLEAN DEFAULT true
		)`,
		`CREATE TABLE usage_records (
			id SERIAL PRIMARY KEY,
			user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
			api_key_id INTEGER,
			endpoint VARCHAR(100), method VARCHAR(10),
			status_code INTEGER, response_time_ms INTEGER,
			ip_address VARCHAR(64), user_agent TEXT,
			billable BOOLEAN NOT NULL DEFAULT true,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE usage_counters (
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			period_kind VARCHAR(5) NOT NULL CHECK (period_kind IN ('day','month')),
			period_start DATE NOT NULL,
			count BIGINT NOT NULL DEFAULT 0,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (user_id, period_kind, period_start)
		)`,
		`INSERT INTO users (email, password_hash, plan_type) VALUES ('counter@example.com', 'x', 'free')`,
		`INSERT INTO api_keys (user_id) SELECT id FROM users WHERE email = 'counter@example.com'`,
	}
	for _, stmt := range schema {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("schema failed on %.60q: %v", stmt, err)
		}
	}

	prev := database.DB
	database.DB = db
	t.Cleanup(func() {
		database.DB = prev
		db.Close()
		if _, err := admin.Exec(fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", counterSchema)); err != nil {
			t.Logf("cleanup: %v", err)
		}
		admin.Close()
	})

	return db
}

func probeUserID(t *testing.T, db *sql.DB) int {
	t.Helper()
	var id int
	if err := db.QueryRow("SELECT id FROM users WHERE email = 'counter@example.com'").Scan(&id); err != nil {
		t.Fatalf("user id: %v", err)
	}
	return id
}

func counterValue(t *testing.T, db *sql.DB, userID int, kind string) int {
	t.Helper()
	var n int
	err := db.QueryRow(`
		SELECT COALESCE(MAX(count), 0) FROM usage_counters
		WHERE user_id = $1 AND period_kind = $2
		  AND period_start = CASE WHEN $2 = 'day' THEN CURRENT_DATE
		                          ELSE date_trunc('month', CURRENT_DATE)::date END
	`, userID, kind).Scan(&n)
	if err != nil {
		t.Fatalf("counter %s: %v", kind, err)
	}
	return n
}

// The counter has to agree with the aggregate it replaced, or the change is a
// silent repricing of everyone's plan.
func TestCountersTrackBillableUsage(t *testing.T) {
	db := setupCounterDB(t)
	userID := probeUserID(t, db)

	for i := 0; i < 7; i++ {
		if err := Auth.RecordUsage(userID, 1, "geocode", "GET", 200, 5, "203.0.113.7", "probe", true); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}
	// Over-limit calls are recorded for the audit trail but must not count
	// against the user again.
	for i := 0; i < 3; i++ {
		if err := Auth.RecordUsage(userID, 1, "geocode", "GET", 429, 1, "203.0.113.7", "probe", false); err != nil {
			t.Fatalf("record non-billable %d: %v", i, err)
		}
	}

	if got := counterValue(t, db, userID, "month"); got != 7 {
		t.Errorf("month counter = %d, want 7", got)
	}
	if got := counterValue(t, db, userID, "day"); got != 7 {
		t.Errorf("day counter = %d, want 7", got)
	}

	status, err := Auth.CheckRateLimitStatus(userID)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.MonthlyUsage != 7 || status.DailyUsage != 7 {
		t.Errorf("status reported month=%d day=%d, want 7/7", status.MonthlyUsage, status.DailyUsage)
	}

	// The audit log keeps every call, billable or not.
	var records int
	if err := db.QueryRow("SELECT COUNT(*) FROM usage_records").Scan(&records); err != nil {
		t.Fatalf("records: %v", err)
	}
	if records != 10 {
		t.Errorf("usage_records = %d, want 10 (the audit trail keeps non-billable calls)", records)
	}
}

// A user with no calls this period has no counter row. That is a zero, not an
// error, and not a reason to reject the request.
func TestStatusWithNoCounterRowIsZero(t *testing.T) {
	db := setupCounterDB(t)
	userID := probeUserID(t, db)

	status, err := Auth.CheckRateLimitStatus(userID)
	if err != nil {
		t.Fatalf("status with no counter row: %v", err)
	}
	if status.MonthlyUsage != 0 || status.DailyUsage != 0 {
		t.Errorf("got month=%d day=%d, want 0/0", status.MonthlyUsage, status.DailyUsage)
	}
	if !status.Within {
		t.Error("a user with no usage must be within their limit")
	}
}

// Incrementing in the database rather than read-modify-write is the whole
// reason concurrent requests cannot lose counts.
func TestConcurrentIncrementsDoNotLoseCounts(t *testing.T) {
	db := setupCounterDB(t)
	userID := probeUserID(t, db)

	const n = 60
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := Auth.RecordUsage(userID, 1, "geocode", "GET", 200, 5, "203.0.113.7", "probe", true); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent record: %v", err)
	}

	if got := counterValue(t, db, userID, "month"); got != n {
		t.Errorf("month counter = %d, want %d -- increments were lost", got, n)
	}
}

// Counters are derived data, so there must be a way back to the source of
// truth when they drift.
func TestRebuildCorrectsDrift(t *testing.T) {
	db := setupCounterDB(t)
	userID := probeUserID(t, db)

	for i := 0; i < 5; i++ {
		if err := Auth.RecordUsage(userID, 1, "geocode", "GET", 200, 5, "203.0.113.7", "probe", true); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	// Simulate an increment that never landed.
	if _, err := db.Exec(`UPDATE usage_counters SET count = 2 WHERE user_id = $1`, userID); err != nil {
		t.Fatalf("introduce drift: %v", err)
	}
	if got := counterValue(t, db, userID, "month"); got != 2 {
		t.Fatalf("drift setup failed: counter = %d, want 2", got)
	}

	if err := Auth.RebuildUsageCounters(); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	if got := counterValue(t, db, userID, "month"); got != 5 {
		t.Errorf("after rebuild month counter = %d, want 5", got)
	}
	if got := counterValue(t, db, userID, "day"); got != 5 {
		t.Errorf("after rebuild day counter = %d, want 5", got)
	}
}

// The rebuild is only a safety net if something can actually call it. It was
// unreachable when first written -- exported, tested, and wired to nothing --
// which would have left counter drift permanent in production.
func TestRebuildIsReachableFromTheAdminRoute(t *testing.T) {
	routes, err := os.ReadFile("../main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if !strings.Contains(string(routes), "handlers.RebuildUsageCountersHandler") {
		t.Error("RebuildUsageCounters has no route; counter drift would be uncorrectable")
	}

	handler, err := os.ReadFile("../handlers/admin_handlers.go")
	if err != nil {
		t.Fatalf("read admin_handlers.go: %v", err)
	}
	if !strings.Contains(string(handler), "func RebuildUsageCountersHandler") {
		t.Error("the route names a handler that does not exist")
	}

	// It must sit on the admin group. Rebuilding recomputes every user's
	// counters, so an ordinary caller must not be able to trigger it.
	adminSection := string(routes)
	idx := strings.Index(adminSection, `admin.POST("/usage-counters/rebuild"`)
	if idx < 0 {
		t.Fatal("rebuild route is not registered on the admin group")
	}
}

// withSearchPathOption adds a libpq "options" parameter setting search_path,
// so the schema scoping rides on each connection rather than on the role.
func withSearchPathOption(dsn, schema string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("options", fmt.Sprintf("-c search_path=%s,public", schema))
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// Migrations run asynchronously and the server serves immediately, so between
// a deploy and migration 22 landing, usage_counters does not exist -- while
// every authenticated request runs this query. Without a fallback the entire
// metered API 500s for the length of the migration chain.
func TestRateLimitSurvivesMissingCounterTable(t *testing.T) {
	db := setupCounterDB(t)
	userID := probeUserID(t, db)

	for i := 0; i < 4; i++ {
		if err := Auth.RecordUsage(userID, 1, "geocode", "GET", 200, 5, "203.0.113.7", "probe", true); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	// Rewind to the pre-migration world.
	if _, err := db.Exec("DROP TABLE usage_counters"); err != nil {
		t.Fatalf("drop counters: %v", err)
	}

	status, err := Auth.CheckRateLimitStatus(userID)
	if err != nil {
		t.Fatalf("rate limit check failed with no counter table: %v", err)
	}
	// The fallback reads usage_records, which still holds all four calls.
	if status.MonthlyUsage != 4 || status.DailyUsage != 4 {
		t.Errorf("fallback reported month=%d day=%d, want 4/4", status.MonthlyUsage, status.DailyUsage)
	}
	if !status.Within {
		t.Error("four calls is inside the free plan; request should not be rejected")
	}
}

// An upsert-only rebuild can raise a counter but never lower one: a stale row
// whose user has no billable records this period produces no row in the
// SELECT, so ON CONFLICT never fires. That is exactly the case an operator
// runs the rebuild for -- someone locked out at a limit they never reached.
func TestRebuildCorrectsAnOverCountDownToZero(t *testing.T) {
	db := setupCounterDB(t)
	userID := probeUserID(t, db)

	// A counter claiming usage that usage_records does not support.
	if _, err := db.Exec(`
		INSERT INTO usage_counters (user_id, period_kind, period_start, count, updated_at)
		VALUES ($1, 'month', date_trunc('month', CURRENT_DATE)::date, 99999, NOW()),
		       ($1, 'day', CURRENT_DATE, 99999, NOW())
	`, userID); err != nil {
		t.Fatalf("seed over-count: %v", err)
	}

	status, err := Auth.CheckRateLimitStatus(userID)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.Within {
		t.Fatal("setup failed: user should be locked out by the bogus counter")
	}

	if err := Auth.RebuildUsageCounters(); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	if got := counterValue(t, db, userID, "month"); got != 0 {
		t.Errorf("month counter = %d after rebuild, want 0 -- the rebuild cannot correct downwards", got)
	}
	status, err = Auth.CheckRateLimitStatus(userID)
	if err != nil {
		t.Fatalf("status after rebuild: %v", err)
	}
	if !status.Within {
		t.Error("user is still locked out after a rebuild that should have cleared the bogus count")
	}
}

// Nothing reads a period once the clock passes it, and usage_records keeps the
// history. Without a purge the table grows by a row per user per active day
// forever.
func TestRebuildPrunesStalePeriods(t *testing.T) {
	db := setupCounterDB(t)
	userID := probeUserID(t, db)

	if _, err := db.Exec(`
		INSERT INTO usage_counters (user_id, period_kind, period_start, count, updated_at)
		VALUES ($1, 'month', (date_trunc('month', CURRENT_DATE) - interval '2 months')::date, 500, NOW()),
		       ($1, 'day', CURRENT_DATE - 45, 20, NOW())
	`, userID); err != nil {
		t.Fatalf("seed old periods: %v", err)
	}

	if err := Auth.RebuildUsageCounters(); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	var stale int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM usage_counters
		WHERE period_start < date_trunc('month', CURRENT_DATE)::date AND user_id = $1
	`, userID).Scan(&stale); err != nil {
		t.Fatalf("count stale: %v", err)
	}
	if stale != 0 {
		t.Errorf("%d stale counter row(s) survived the rebuild", stale)
	}
}
