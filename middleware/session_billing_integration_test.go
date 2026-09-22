package middleware

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"geocoding-api/database"

	"github.com/labstack/echo/v4"
	_ "github.com/lib/pq"
)

const sessionBillingSchema = "session_billing_probe"

// What the feature is for, end to end: the keystrokes of one lookup reach
// usage_records as a single billed call.
//
// The unit tests prove the tracker decides correctly; this proves the
// decision is the one the billing path uses. Without it, a middleware that
// computed the right answer and then recorded every call as billable anyway
// would pass everything else.
func TestSessionCallsAreRecordedUnbilled(t *testing.T) {
	dsn := os.Getenv("PROBE_DSN")
	if dsn == "" {
		t.Skip("PROBE_DSN not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		t.Skipf("probe database unreachable: %v", err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec("DROP SCHEMA IF EXISTS " + sessionBillingSchema + " CASCADE"); err != nil {
			t.Logf("cleanup: %v", err)
		}
		db.Close()
	})

	// The key's secret is hashed the way the service hashes it, so
	// ValidateAPIKey finds this row rather than a real one.
	const (
		secret = "gk_session_billing_probe_key_value_0000000000000000000000000000"
		// A second customer's key, to prove a token opens a session for the
		// key that used it rather than for everyone.
		otherSecret = "gk_session_billing_probe_other_key_00000000000000000000000000"
	)
	for _, stmt := range []string{
		"DROP SCHEMA IF EXISTS " + sessionBillingSchema + " CASCADE",
		"CREATE SCHEMA " + sessionBillingSchema,
		"SET search_path TO " + sessionBillingSchema,
		`CREATE TABLE users (
			id SERIAL PRIMARY KEY, email VARCHAR(255) NOT NULL UNIQUE,
			password_hash VARCHAR(255) NOT NULL, name VARCHAR(255), company VARCHAR(255),
			plan_type VARCHAR(50) DEFAULT 'free',
			is_active BOOLEAN DEFAULT true, is_admin BOOLEAN DEFAULT false,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP, updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE api_keys (
			id SERIAL PRIMARY KEY, user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
			name VARCHAR(255) NOT NULL, key_hash VARCHAR(255) NOT NULL UNIQUE,
			permissions TEXT[], is_active BOOLEAN DEFAULT true, last_used_at TIMESTAMP,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP, updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			key_preview VARCHAR(255), expires_at TIMESTAMP,
			monthly_limit INTEGER, daily_limit INTEGER)`,
		// The rate-limit check left-joins this, so it has to exist even empty.
		`CREATE TABLE subscriptions (
			id SERIAL PRIMARY KEY, user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
			plan_type VARCHAR(50), monthly_limit INTEGER, is_active BOOLEAN DEFAULT true)`,
		`CREATE TABLE usage_records (
			id SERIAL PRIMARY KEY, user_id INTEGER, api_key_id INTEGER,
			endpoint VARCHAR(100), method VARCHAR(10), status_code INTEGER,
			response_time_ms INTEGER, ip_address VARCHAR(64), user_agent TEXT,
			billable BOOLEAN DEFAULT true, units INTEGER NOT NULL DEFAULT 1,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE usage_counters (
			user_id INTEGER NOT NULL, period_kind VARCHAR(5) NOT NULL,
			period_start DATE NOT NULL, count BIGINT NOT NULL DEFAULT 0,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (user_id, period_kind, period_start))`,
		`CREATE TABLE api_key_counters (
			api_key_id INTEGER NOT NULL, period_kind VARCHAR(5) NOT NULL,
			period_start DATE NOT NULL, count BIGINT NOT NULL DEFAULT 0,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (api_key_id, period_kind, period_start))`,
		`INSERT INTO users (id, email, password_hash, name, company, plan_type)
			VALUES (1, 'probe@example.test', 'x', 'Probe', 'Probe Co', 'free')`,
		`INSERT INTO users (id, email, password_hash, name, company, plan_type)
			VALUES (2, 'other@example.test', 'x', 'Other', 'Other Co', 'free')`,
		fmt.Sprintf(`INSERT INTO api_keys (id, user_id, name, key_hash, permissions, is_active, key_preview)
			VALUES (1, 1, 'probe', encode(sha256(%s), 'hex'), ARRAY['search','addresses'], true, 'gk_probe...'),
			       (2, 2, 'other', encode(sha256(%s), 'hex'), ARRAY['search','addresses'], true, 'gk_other...')`,
			quoteBytes(secret), quoteBytes(otherSecret)),
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("setup failed on %.70q: %v", stmt, err)
		}
	}

	prev := database.DB
	database.DB = db
	t.Cleanup(func() { database.DB = prev })

	e := echo.New()
	e.Use(APIKeyAuth())
	e.GET("/api/v1/addresses/search", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"ok": "yes"})
	})

	callAs := func(t *testing.T, key, url string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("X-API-Key", key)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d, body %s", url, rec.Code, rec.Body.String())
		}
		return rec
	}
	call := func(t *testing.T, url string) *httptest.ResponseRecorder {
		t.Helper()
		return callAs(t, secret, url)
	}

	const token = "session-billing-probe-token"
	first := call(t, "/api/v1/addresses/search?q=ma&session="+token)
	if got := first.Header().Get("X-Session-Billed"); got != "true" {
		t.Errorf("first call X-Session-Billed = %q, want true", got)
	}
	for i := 0; i < 4; i++ {
		rec := call(t, fmt.Sprintf("/api/v1/addresses/search?q=main%d&session=%s", i, token))
		if got := rec.Header().Get("X-Session-Billed"); got != "false" {
			t.Errorf("keystroke %d X-Session-Billed = %q, want false", i+2, got)
		}
	}
	// The same token from another customer's key: a session belongs to the
	// key that opened it, so this is a new lookup and is billed.
	other := callAs(t, otherSecret, "/api/v1/addresses/search?q=ma&session="+token)
	if got := other.Header().Get("X-Session-Billed"); got != "true" {
		t.Errorf("another key's call X-Session-Billed = %q, want true -- it rode the first key's session", got)
	}

	// One more lookup, no token: billed like any other call.
	call(t, "/api/v1/addresses/search?q=elm")

	// RecordUsage runs after the response, so the rows arrive shortly after.
	var billed, unbilled int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := db.QueryRow(`SELECT
			count(*) FILTER (WHERE billable),
			count(*) FILTER (WHERE NOT billable) FROM usage_records`).Scan(&billed, &unbilled); err != nil {
			t.Fatal(err)
		}
		if billed+unbilled == 7 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if billed != 3 || unbilled != 4 {
		t.Errorf("recorded %d billed and %d free, want 3 and 4: five keystrokes are one lookup, plus another key's call and one ordinary call",
			billed, unbilled)
	}

	// The quota counts the lookups, not the keystrokes.
	var counted int
	if err := db.QueryRow(`SELECT COALESCE(SUM(count), 0) FROM usage_counters WHERE period_kind = 'month'`).Scan(&counted); err != nil {
		t.Fatal(err)
	}
	// Two for this key's owner: the session and the plain call. The other
	// customer's single call is counted against their own user.
	if counted != 3 {
		t.Errorf("quota counted %d calls across both users, want 3", counted)
	}
}

// quoteBytes renders a Go string as a Postgres bytea literal for sha256().
func quoteBytes(s string) string {
	return fmt.Sprintf("'\\x%x'::bytea", s)
}
