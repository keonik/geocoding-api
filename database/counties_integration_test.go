package database

import (
	"database/sql"
	"os"
	"testing"

	_ "github.com/lib/pq"
)

// NOTE on running these: with Colima, `docker run -p` publishes through an ssh
// forward, and that forward accepts connections before Postgres is reachable
// through it -- so a probe started right after `pg_isready` fails with a bare
// EOF. Wait for a real handshake on 127.0.0.1 (not localhost, whose ::1 route
// is flaky here) before running.
//
// EnsureCountyBoundaries must be a no-op once the table has rows -- that is
// what makes it safe to run on every boot. The empty-table path is not
// exercised here on purpose: it reaches out to download source data and writes
// placeholder files into the working tree.
func TestEnsureCountyBoundariesProbe(t *testing.T) {
	dsn := os.Getenv("PROBE_DSN")
	if dsn == "" {
		t.Skip("PROBE_DSN not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	var exists bool
	if err := db.QueryRow(`SELECT to_regclass('ohio_counties') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatalf("probe: %v", err)
	}
	if !exists {
		t.Skip("probe database has no ohio_counties table")
	}

	prev := DB
	DB = db
	defer func() { DB = prev }()

	if _, err := db.Exec(`DELETE FROM ohio_counties`); err != nil {
		t.Fatalf("reset: %v", err)
	}

	// Empty table reads 0 rather than erroring on a NULL aggregate.
	n, err := CountOhioCounties()
	if err != nil || n != 0 {
		t.Fatalf("empty: got (%d, %v), want (0, nil)", n, err)
	}

	if _, err := db.Exec(`
		INSERT INTO ohio_counties (county_name, source_name, layer, address_count, bounds_geometry)
		VALUES ('Franklin','tiger','addresses',12345,
		        ST_SetSRID(ST_GeomFromText('POLYGON((-83.2 39.8,-82.8 39.8,-82.8 40.1,-83.2 40.1,-83.2 39.8))'),4326))
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if n, err = CountOhioCounties(); err != nil || n != 1 {
		t.Fatalf("seeded: got (%d, %v), want (1, nil)", n, err)
	}

	// With rows present this must return without touching the loader at all.
	if err := EnsureCountyBoundaries(); err != nil {
		t.Fatalf("EnsureCountyBoundaries with rows present: %v", err)
	}
	if n, err = CountOhioCounties(); err != nil || n != 1 {
		t.Fatalf("row count changed: got (%d, %v), want (1, nil)", n, err)
	}
	if _, err := os.Stat("oh"); err == nil {
		t.Error("an oh/ directory was created; the loader ran when it should not have")
	}
}
