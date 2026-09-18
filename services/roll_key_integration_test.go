package services

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"geocoding-api/database"

	_ "github.com/lib/pq"
)

// RollAPIKey's whole reason to exist is that the key row survives, so the
// assertions here are mostly about what must NOT change. Skipped unless
// PROBE_DSN is set.
func TestRollAPIKeyProbe(t *testing.T) {
	dsn := os.Getenv("PROBE_DSN")
	if dsn == "" {
		t.Skip("PROBE_DSN not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// search_path is per-session, so every statement must use this connection.
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		t.Skipf("probe database unreachable: %v", err)
	}

	// This test used to roll API key 1 in whatever database PROBE_DSN named.
	// Pointed at a real one, that rotates a live customer's key: the old secret
	// stops validating at once, and the new one is returned to this test and
	// thrown away. Their integration breaks and there is nothing to restore.
	//
	// It now builds its own keys in a private schema. Key 1 is active with
	// usage history to preserve, key 2 is the same user's revoked key, and key 3
	// belongs to someone else -- the three cases the assertions below need.
	const schema = "roll_key_probe"
	for _, stmt := range []string{
		"DROP SCHEMA IF EXISTS " + schema + " CASCADE",
		"CREATE SCHEMA " + schema,
		"SET search_path TO " + schema,
		`CREATE TABLE users (
			id SERIAL PRIMARY KEY,
			email VARCHAR(255) NOT NULL UNIQUE,
			password_hash VARCHAR(255) NOT NULL,
			plan_type VARCHAR(50) DEFAULT 'free',
			is_active BOOLEAN DEFAULT true,
			is_admin BOOLEAN DEFAULT false,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE api_keys (
			id SERIAL PRIMARY KEY,
			user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
			name VARCHAR(255) NOT NULL,
			key_hash VARCHAR(255) NOT NULL UNIQUE,
			permissions TEXT[],
			is_active BOOLEAN DEFAULT true,
			last_used_at TIMESTAMP,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			key_preview VARCHAR(255),
			expires_at TIMESTAMP)`,
		`CREATE TABLE usage_records (
			id SERIAL PRIMARY KEY,
			user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
			api_key_id INTEGER REFERENCES api_keys(id) ON DELETE CASCADE,
			endpoint VARCHAR(100), method VARCHAR(10),
			billable BOOLEAN DEFAULT true,
			units INTEGER NOT NULL DEFAULT 1,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`,
		`INSERT INTO users (id, email, password_hash) VALUES
			(1, 'owner@example.test', 'x'),
			(2, 'someone-else@example.test', 'x')`,
		`INSERT INTO api_keys (id, user_id, name, key_hash, permissions, is_active, key_preview) VALUES
			(1, 1, 'production', 'hash-one',   ARRAY['geocode','search'], true,  'gk_one...'),
			(2, 1, 'old',        'hash-two',   ARRAY['geocode'],          false, 'gk_two...'),
			(3, 2, 'theirs',     'hash-three', ARRAY['geocode'],          true,  'gk_thr...')`,
		`INSERT INTO usage_records (user_id, api_key_id, endpoint, method) VALUES
			(1, 1, 'geocode', 'GET'), (1, 1, 'search', 'GET'), (1, 1, 'geocode', 'GET')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("setup failed on %.60q: %v", stmt, err)
		}
	}
	t.Cleanup(func() {
		if _, err := db.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE"); err != nil {
			t.Logf("cleanup: %v", err)
		}
		db.Close()
	})

	prev := database.DB
	database.DB = db
	defer func() { database.DB = prev }()

	as := &AuthService{}

	type row struct {
		name      string
		hash      string
		preview   string
		perms     string
		createdAt string
		usage     int
	}
	read := func(t *testing.T, id int) row {
		t.Helper()
		var r row
		err := db.QueryRow(`
			SELECT k.name, k.key_hash, k.key_preview, k.permissions::text, k.created_at::text,
			       (SELECT count(*) FROM usage_records u WHERE u.api_key_id = k.id)
			FROM api_keys k WHERE k.id = $1`, id).
			Scan(&r.name, &r.hash, &r.preview, &r.perms, &r.createdAt, &r.usage)
		if err != nil {
			t.Fatalf("read key %d: %v", id, err)
		}
		return r
	}

	before := read(t, 1)
	if before.usage == 0 {
		t.Fatal("fixture has no usage rows to preserve")
	}

	key, secret, err := as.RollAPIKey(1, 1)
	if err != nil {
		t.Fatalf("RollAPIKey: %v", err)
	}
	after := read(t, 1)

	// --- what must change ---
	if after.hash == before.hash {
		t.Error("key_hash unchanged: the old secret would still validate")
	}
	if after.preview == before.preview {
		t.Error("key_preview unchanged")
	}
	sum := sha256.Sum256([]byte(secret))
	if want := hex.EncodeToString(sum[:]); after.hash != want {
		t.Errorf("stored hash does not match the returned secret")
	}
	if !strings.HasPrefix(secret, "gk_") || len(secret) != 67 {
		t.Errorf("secret has the wrong shape: len=%d prefix=%q", len(secret), secret[:3])
	}

	// --- what must not ---
	if after.name != before.name {
		t.Errorf("name changed: %q -> %q", before.name, after.name)
	}
	if after.perms != before.perms {
		t.Errorf("permissions changed: %s -> %s", before.perms, after.perms)
	}
	if after.createdAt != before.createdAt {
		t.Errorf("created_at changed: %s -> %s", before.createdAt, after.createdAt)
	}
	if after.usage != before.usage {
		t.Errorf("usage history lost: %d -> %d rows", before.usage, after.usage)
	}
	if key.ID != 1 {
		t.Errorf("returned key id = %d, want 1 (same row)", key.ID)
	}

	// --- authorisation ---
	if _, _, err := as.RollAPIKey(1, 3); err == nil {
		t.Error("rolled another user's key")
	}
	if _, _, err := as.RollAPIKey(1, 2); err == nil {
		t.Error("rolled a revoked key")
	}

	// Rolling twice must not collide or reuse a secret.
	_, second, err := as.RollAPIKey(1, 1)
	if err != nil {
		t.Fatalf("second roll: %v", err)
	}
	if second == secret {
		t.Error("two rolls produced the same secret")
	}
}
