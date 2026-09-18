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
	// One connection, because search_path is per-session and every statement
	// below has to land on the probe schema.
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		t.Skipf("probe database unreachable: %v", err)
	}

	// This test used to DELETE FROM ohio_counties against whatever PROBE_DSN
	// named. Pointed at a real database -- and a real connection string is the
	// obvious thing to reach for when chasing a production bug -- that wiped
	// every county boundary. It also raced any other package testing against
	// the same database, which is how it showed up: a county probe elsewhere
	// read its fixture mid-reload and failed.
	//
	// It now builds its own table in a private schema and never names public.
	// That also means it always runs, instead of skipping whenever the target
	// happened to lack the table.
	const schema = "counties_probe"
	for _, stmt := range []string{
		"CREATE EXTENSION IF NOT EXISTS postgis",
		"DROP SCHEMA IF EXISTS " + schema + " CASCADE",
		"CREATE SCHEMA " + schema,
		// public stays on the path for the PostGIS functions. The table is
		// created in the probe schema, so it shadows any public copy and
		// nothing below resolves past it.
		"SET search_path TO " + schema + ", public",
		`CREATE TABLE ohio_counties (
			id SERIAL PRIMARY KEY,
			county_name VARCHAR(255) UNIQUE NOT NULL,
			source_name VARCHAR(255) NOT NULL,
			layer VARCHAR(100) NOT NULL,
			address_count INTEGER DEFAULT 0,
			stats JSONB,
			bounds_geometry GEOMETRY(POLYGON, 4326) NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
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

	prev := DB
	DB = db
	defer func() { DB = prev }()

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
