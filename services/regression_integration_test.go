package services

import (
	"database/sql"
	"os"
	"testing"

	"geocoding-api/database"
	"geocoding-api/models"

	_ "github.com/lib/pq"
)

// requireTables skips when the probe database lacks the fixture a test needs.
// Several probe tests share PROBE_DSN but want different schemas; without this
// they fail confusingly on someone else's fixture instead of standing aside.
func requireTables(t *testing.T, db *sql.DB, tables ...string) {
	t.Helper()
	for _, name := range tables {
		var exists bool
		if err := db.QueryRow(`SELECT to_regclass($1) IS NOT NULL`, name).Scan(&exists); err != nil {
			t.Fatalf("checking for table %s: %v", name, err)
		}
		if !exists {
			t.Skipf("probe database has no %s table", name)
		}
	}
}

// requireColumns skips when a fixture table exists but lacks a column the
// test depends on -- table presence alone is not enough to say a fixture fits.
// requireFixture skips unless the probe database holds the data a probe's
// assertions were written against.
//
// These probes assert literal values -- four states, Texas present, four keys
// for user 1 -- tied to one hand-seeded fixture. Pointed at any other database
// they fail every assertion at once, which says the data differs rather than
// that anything is broken. A suite that is always red that way trains people to
// ignore red, which is how a genuine failure gets missed. Checking up front
// turns it into a skip that names what was expected.
func requireFixture(t *testing.T, db *sql.DB, what, query string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow(query).Scan(&got); err != nil {
		t.Skipf("cannot check fixture (%s): %v", what, err)
	}
	if got != want {
		t.Skipf("probe expects %s = %d, database has %d; its assertions are written for that fixture", what, want, got)
	}
}

func requireColumns(t *testing.T, db *sql.DB, table string, columns ...string) {
	t.Helper()
	for _, col := range columns {
		var exists bool
		err := db.QueryRow(`SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name = $1 AND column_name = $2)`, table, col).Scan(&exists)
		if err != nil {
			t.Fatalf("checking for %s.%s: %v", table, col, err)
		}
		if !exists {
			t.Skipf("probe database has no %s.%s column", table, col)
		}
	}
}

func probeDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("PROBE_DSN")
	if dsn == "" {
		t.Skip("PROBE_DSN not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return db
}

// SearchStates built its count query with args[:len(args)-2], assuming LIMIT
// and OFFSET were both always appended. OFFSET is conditional, so an
// unfiltered request left one arg and the slice bound went negative -- a
// panic, which surfaced as Echo's generic 500. Every unfiltered /states
// request was failing in production.
func TestSearchStatesUnfilteredProbe(t *testing.T) {
	db := probeDB(t)
	defer db.Close()
	prev := database.DB
	database.DB = db
	defer func() { database.DB = prev }()

	requireTables(t, db, "us_states")
	requireFixture(t, db, "rows in us_states", "SELECT COUNT(*) FROM us_states", 4)

	ss := &StateService{}

	cases := []struct {
		name      string
		params    models.StateSearchParams
		wantCount int
		wantTotal int
	}{
		// The panicking case: no filters, no offset -> args is just [limit].
		{"no filters", models.StateSearchParams{}, 4, 4},
		{"limit only", models.StateSearchParams{Limit: 1}, 1, 4},
		// Filtered: count must reflect the filter, not the page size. This was
		// also wrong before -- the count query got zero args for a one-arg
		// predicate, errored, and silently fell back to len(states).
		{"name filter", models.StateSearchParams{Name: "Ohio"}, 1, 1},
		{"abbr filter", models.StateSearchParams{Abbr: "TX"}, 1, 1},
		{"filter + offset", models.StateSearchParams{Region: "3", Offset: 1}, 1, 2},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := ss.SearchStates(c.params)
			if err != nil {
				t.Fatalf("SearchStates: %v", err)
			}
			if len(res.States) != c.wantCount {
				t.Errorf("returned %d states, want %d", len(res.States), c.wantCount)
			}
			if res.Total != c.wantTotal {
				t.Errorf("Total = %d, want %d", res.Total, c.wantTotal)
			}
		})
	}
}

// CountyService captured database.DB in a package-level init(), which runs
// before main() connects -- so the handle was nil and every county endpoint
// panicked. A service with no injected handle must fall through to the global.
func TestCountyServiceNilHandleProbe(t *testing.T) {
	db := probeDB(t)
	defer db.Close()
	prev := database.DB
	database.DB = db
	defer func() { database.DB = prev }()

	requireTables(t, db, "ohio_counties")
	requireFixture(t, db, "rows in ohio_counties", "SELECT COUNT(*) FROM ohio_counties", 1)

	// Exactly what init() produces when it runs before the connection exists.
	cs := &CountyService{db: nil}

	counties, err := cs.GetAllCounties(models.CountySearchParams{})
	if err != nil {
		t.Fatalf("GetAllCounties with a nil handle: %v", err)
	}
	if len(counties) != 1 {
		t.Errorf("got %d counties, want 1", len(counties))
	}

	b, err := cs.GetCountyBoundaryGeoJSON("Franklin", DefaultBoundaryTolerance, 6)
	if err != nil {
		t.Fatalf("GetCountyBoundaryGeoJSON with a nil handle: %v", err)
	}
	if len(b.Features) != 1 || len(b.Features[0].Geometry) < 20 {
		t.Errorf("boundary came back empty: %+v", b)
	}
}
