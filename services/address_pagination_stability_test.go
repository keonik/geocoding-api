package services

import (
	"database/sql"
	"fmt"
	"os"
	"testing"

	"geocoding-api/models"

	_ "github.com/lib/pq"
)

const tieTestSchema = "addr_tie_probe"

// Rows that every ORDER BY in searchAddresses considers equal: same county,
// city, street and house number, and the same coordinate. Real data looks like
// this constantly -- the units in one apartment building share a street
// address and a single building point.
const tiedRowCount = 120

// setupTiedDB seeds a table where sorting cannot distinguish most rows.
//
// Lives in its own schema for the same reason setupCountTestDB does: PROBE_DSN
// may point at a database holding real addresses, and this must never touch
// them.
func setupTiedDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := os.Getenv("PROBE_DSN")
	if dsn == "" {
		t.Skip("PROBE_DSN not set")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// search_path is per-connection, so pin the pool to one connection.
	db.SetMaxOpenConns(1)

	if err := db.Ping(); err != nil {
		t.Skipf("probe database unreachable: %v", err)
	}

	stmts := []string{
		"CREATE EXTENSION IF NOT EXISTS postgis",
		"CREATE EXTENSION IF NOT EXISTS pg_trgm",
		fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", tieTestSchema),
		fmt.Sprintf("CREATE SCHEMA %s", tieTestSchema),
		fmt.Sprintf("SET search_path TO %s, public", tieTestSchema),
		`CREATE TABLE ohio_addresses (
			id BIGSERIAL PRIMARY KEY,
			hash VARCHAR(255) UNIQUE NOT NULL,
			house_number VARCHAR(50),
			street VARCHAR(255),
			unit VARCHAR(50),
			city VARCHAR(255),
			district VARCHAR(10),
			region VARCHAR(2),
			postcode VARCHAR(10),
			county VARCHAR(255),
			geom GEOMETRY(POINT, 4326) NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			full_address TEXT
		)`,
		// Only the unit and the hash differ. Everything the sort keys look at
		// is identical across all rows.
		fmt.Sprintf(`INSERT INTO ohio_addresses
			(hash, house_number, street, unit, city, district, region, postcode, county, geom, full_address)
		 SELECT
			md5(i::text),
			'500',
			'Barendt Road',
			'Apt ' || i::text,
			'Columbus',
			'FRA',
			'OH',
			'43004',
			'Franklin',
			ST_SetSRID(ST_MakePoint(-83.0, 40.0), 4326),
			'500 Barendt Road, Columbus, OH 43004'
		 FROM generate_series(1, %d) AS i`, tiedRowCount),
		`ALTER TABLE ohio_addresses
			ADD COLUMN fts tsvector
			GENERATED ALWAYS AS (to_tsvector('simple', coalesce(full_address, ''))) STORED`,
		"CREATE INDEX ON ohio_addresses USING gin (fts)",
		"CREATE INDEX ON ohio_addresses USING gin (full_address gin_trgm_ops)",
		"CREATE INDEX ON ohio_addresses (county)",
		"ANALYZE ohio_addresses",
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("setup failed on %.60q: %v", s, err)
		}
	}

	t.Cleanup(func() {
		if _, err := db.Exec(fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", tieTestSchema)); err != nil {
			t.Logf("cleanup: %v", err)
		}
		db.Close()
	})

	return db
}

// walkPages paginates the whole result set and returns the ids in page order.
func walkPages(t *testing.T, svc *AddressService, base models.AddressSearchParams, pageSize int) []int64 {
	t.Helper()

	var ids []int64
	for offset := 0; ; offset += pageSize {
		params := base
		params.Limit = pageSize
		params.Offset = offset

		rows, _, err := svc.SearchAddresses(params)
		if err != nil {
			t.Fatalf("search at offset %d: %v", offset, err)
		}
		if len(rows) == 0 {
			break
		}
		for _, r := range rows {
			ids = append(ids, r.ID)
		}
		if offset > tiedRowCount*4 {
			t.Fatal("pagination did not terminate")
		}
	}
	return ids
}

// A paginated walk must visit every row exactly once. With no tiebreaker the
// database is free to order tied rows differently on each page query, so a row
// can land on two consecutive pages and another on none -- the client sees a
// duplicate and a silent omission.
func TestPaginatedWalkVisitsEveryRowExactlyOnce(t *testing.T) {
	db := setupTiedDB(t)
	svc := NewAddressService(db)

	cases := []struct {
		name   string
		params models.AddressSearchParams
	}{
		{
			name:   "default ordering",
			params: models.AddressSearchParams{},
		},
		{
			name:   "relevance ordering",
			params: models.AddressSearchParams{Query: "Barendt"},
		},
		{
			name:   "distance ordering",
			params: models.AddressSearchParams{Lat: 40.0, Lng: -83.0},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ids := walkPages(t, svc, tc.params, 10)

			if len(ids) != tiedRowCount {
				t.Errorf("walk returned %d rows, want %d", len(ids), tiedRowCount)
			}

			seen := make(map[int64]int, len(ids))
			for _, id := range ids {
				seen[id]++
			}
			var dupes []int64
			for id, n := range seen {
				if n > 1 {
					dupes = append(dupes, id)
				}
			}
			if len(dupes) > 0 {
				t.Errorf("%d row(s) appeared on more than one page: %v", len(dupes), dupes)
			}
			if len(seen) != tiedRowCount {
				t.Errorf("walk covered %d distinct rows, want %d (%d were skipped entirely)",
					len(seen), tiedRowCount, tiedRowCount-len(seen))
			}
		})
	}
}

// The same query must return the same page every time. This is the property
// LIMIT/OFFSET pagination is built on, and without a total order the planner
// is under no obligation to provide it -- a different LIMIT is enough to
// change which rows come back first.
func TestOrderingIsReproducibleAcrossQueries(t *testing.T) {
	db := setupTiedDB(t)
	svc := NewAddressService(db)

	first := func(limit int) []int64 {
		rows, _, err := svc.SearchAddresses(models.AddressSearchParams{
			Lat: 40.0, Lng: -83.0, Limit: limit,
		})
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		var ids []int64
		for _, r := range rows {
			ids = append(ids, r.ID)
		}
		return ids
	}

	small := first(10)
	large := first(100)

	if len(small) != 10 || len(large) != 100 {
		t.Fatalf("unexpected row counts: %d, %d", len(small), len(large))
	}

	// The first 10 of a 100-row fetch must be the same 10, in the same order,
	// as a 10-row fetch of the same query.
	for i := range small {
		if small[i] != large[i] {
			t.Fatalf("position %d differs between LIMIT 10 and LIMIT 100: %d vs %d\n"+
				"the ordering is not a total order, so pages are not reproducible",
				i, small[i], large[i])
		}
	}
}
