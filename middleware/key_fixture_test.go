package middleware

import (
	"database/sql"
	"fmt"
	"os"
	"testing"

	"geocoding-api/database"

	_ "github.com/lib/pq"
)

// The two keys every middleware test here needs: one customer's, and another's
// to prove nothing is shared between them. The secrets are hashed the way the
// service hashes them, so ValidateAPIKey finds these rows rather than a real
// deployment's.
const (
	probeSecret      = "gk_session_billing_probe_key_value_0000000000000000000000000000"
	probeOtherSecret = "gk_session_billing_probe_other_key_00000000000000000000000000"
)

// setupKeyFixture builds a private schema holding two customers' keys and
// points database.DB at it for the duration of the test.
func setupKeyFixture(t *testing.T, schema string) *sql.DB {
	t.Helper()
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
		if _, err := db.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE"); err != nil {
			t.Logf("cleanup: %v", err)
		}
		db.Close()
	})

	for _, stmt := range []string{
		"DROP SCHEMA IF EXISTS " + schema + " CASCADE",
		"CREATE SCHEMA " + schema,
		"SET search_path TO " + schema,
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
		`INSERT INTO users (id, email, password_hash, name, company, plan_type) VALUES
			(1, 'probe@example.test', 'x', 'Probe', 'Probe Co', 'free'),
			(2, 'other@example.test', 'x', 'Other', 'Other Co', 'free')`,
		fmt.Sprintf(`INSERT INTO api_keys (id, user_id, name, key_hash, permissions, is_active, key_preview)
			VALUES (1, 1, 'probe', encode(sha256(%s), 'hex'), ARRAY['search','addresses','counties'], true, 'gk_probe...'),
			       (2, 2, 'other', encode(sha256(%s), 'hex'), ARRAY['search','addresses'], true, 'gk_other...')`,
			pgBytes(probeSecret), pgBytes(probeOtherSecret)),
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("setup failed on %.70q: %v", stmt, err)
		}
	}

	prev := database.DB
	database.DB = db
	t.Cleanup(func() { database.DB = prev })
	return db
}

// pgBytes renders a Go string as a Postgres bytea literal, for sha256().
func pgBytes(s string) string {
	return fmt.Sprintf("'\\x%x'::bytea", s)
}
