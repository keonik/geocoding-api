package services

import (
	"database/sql"
	"fmt"
	"os"
	"testing"

	"geocoding-api/database"

	_ "github.com/lib/pq"
)

const keyQuotaSchema = "key_quota_probe"

// setupKeyQuotaDB builds users, keys, usage and both counter tables in a
// private schema. The search_path is the probe schema alone -- nothing here uses
// PostGIS, and a trailing public would let a dropped table read through to the
// real one, which is exactly how an earlier test here silently stopped testing
// what it claimed to.
func setupKeyQuotaDB(t *testing.T) *sql.DB {
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

	for _, stmt := range []string{
		"DROP SCHEMA IF EXISTS " + keyQuotaSchema + " CASCADE",
		"CREATE SCHEMA " + keyQuotaSchema,
		"SET search_path TO " + keyQuotaSchema,
		`CREATE TABLE users (
			id SERIAL PRIMARY KEY, email VARCHAR(255) NOT NULL UNIQUE,
			password_hash VARCHAR(255) NOT NULL, plan_type VARCHAR(50) DEFAULT 'pro',
			is_active BOOLEAN DEFAULT true, is_admin BOOLEAN DEFAULT false)`,
		`CREATE TABLE api_keys (
			id SERIAL PRIMARY KEY, user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
			name VARCHAR(255) NOT NULL, key_hash VARCHAR(255) NOT NULL UNIQUE,
			is_active BOOLEAN DEFAULT true, updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			monthly_limit INTEGER, daily_limit INTEGER,
			CHECK ((monthly_limit IS NULL OR monthly_limit > 0) AND (daily_limit IS NULL OR daily_limit > 0)))`,
		`CREATE TABLE usage_records (
			id SERIAL PRIMARY KEY, user_id INTEGER, api_key_id INTEGER,
			endpoint VARCHAR(100), method VARCHAR(10), status_code INTEGER,
			response_time_ms INTEGER, ip_address VARCHAR(64), user_agent TEXT,
			billable BOOLEAN NOT NULL DEFAULT true, units INTEGER NOT NULL DEFAULT 1,
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
		`INSERT INTO users (id, email, password_hash) VALUES
			(1, 'owner@example.test', 'x'), (2, 'other@example.test', 'x')`,
		`INSERT INTO api_keys (id, user_id, name, key_hash) VALUES
			(1, 1, 'production', 'h-prod'), (2, 1, 'staging', 'h-staging')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("setup failed on %.60q: %v", stmt, err)
		}
	}

	prev := database.DB
	database.DB = db
	t.Cleanup(func() {
		database.DB = prev
		if _, err := db.Exec(fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", keyQuotaSchema)); err != nil {
			t.Logf("cleanup: %v", err)
		}
		db.Close()
	})
	return db
}
