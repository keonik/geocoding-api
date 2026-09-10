package database

import (
	"os"
	"testing"
)

// Temporary harness: applies the real migration registry to a scratch database
// so migration ordering can be verified end to end. Deleted after use.
func TestTmpApplyMigrations(t *testing.T) {
	if os.Getenv("MIGRATE_IT") != "1" {
		t.Skip("MIGRATE_IT not set")
	}
	// Migration 16 reads migrations/*.sql by a path relative to the process
	// CWD, which in production is the app root. go test runs in the package
	// dir, so step up one level first.
	if err := os.Chdir(".."); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	if err := InitDB(); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer CloseDB()
	if err := RunMigrations(); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	v, err := AppliedMigrationVersion()
	if err != nil {
		t.Fatalf("AppliedMigrationVersion: %v", err)
	}
	t.Logf("applied schema version = %d", v)
}
