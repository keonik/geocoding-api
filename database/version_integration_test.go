package database

import (
	"database/sql"
	"os"
	"testing"

	_ "github.com/lib/pq"
)

// AppliedMigrationVersion is what /health reports for deploy verification, so
// its two edge cases matter: an empty table must read 0 rather than erroring
// on a NULL aggregate. Skipped unless PROBE_DSN is set.
//
// Use 127.0.0.1 in the DSN, not localhost: localhost resolves ::1 first and
// Docker's IPv6 loopback publish drops the connection with a bare EOF.
func TestAppliedMigrationVersionProbe(t *testing.T) {
	dsn := os.Getenv("PROBE_DSN")
	if dsn == "" {
		t.Skip("PROBE_DSN not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	prev := DB
	DB = db
	defer func() { DB = prev }()

	// MAX() over no rows is NULL, not 0.
	if v, err := AppliedMigrationVersion(); err != nil || v != 0 {
		t.Fatalf("empty table: got (%d, %v), want (0, nil)", v, err)
	}

	if _, err := db.Exec(`INSERT INTO schema_migrations (version, description) VALUES (1,'a'),(19,'b'),(7,'c')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	v, err := AppliedMigrationVersion()
	if err != nil {
		t.Fatalf("populated: %v", err)
	}
	if v != 19 {
		t.Errorf("got %d, want 19 (the max, not the last inserted)", v)
	}
}
