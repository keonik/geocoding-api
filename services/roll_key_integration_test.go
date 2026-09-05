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
	defer db.Close()

	prev := database.DB
	database.DB = db
	defer func() { database.DB = prev }()

	requireTables(t, db, "api_keys", "users", "usage_records")

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
