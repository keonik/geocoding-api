package services

import (
	"database/sql"
	"fmt"
	"os"
	"testing"

	"geocoding-api/models"

	_ "github.com/lib/pq"
)

// The single-pass count change replaced a standalone `SELECT COUNT(*)` with
// `COUNT(*) OVER ()` riding along on the main query, for filtered searches only.
// These tests pin the property that had to survive that: for any params, `total`
// is the number of rows matching the WHERE clause, independent of LIMIT/OFFSET,
// and the rows themselves are unchanged.
//
// The oracle is a hand-written COUNT for each case rather than the previous
// implementation, so the assertions do not depend on either version of the
// builder being correct -- only on the SQL predicate the case describes.
//
// Everything runs inside a private schema so that pointing PROBE_DSN at a
// database with real data cannot drop or overwrite ohio_addresses. Skipped
// unless PROBE_DSN is set.

const countTestSchema = "addr_count_test"

func setupCountTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := os.Getenv("PROBE_DSN")
	if dsn == "" {
		t.Skip("PROBE_DSN not set")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	// search_path is a per-connection setting, so the pool is pinned to one
	// connection. Without this, a query could land on a fresh connection whose
	// search_path still points at public and hit the real ohio_addresses.
	db.SetMaxOpenConns(1)

	if err := db.Ping(); err != nil {
		t.Skipf("probe database unreachable: %v", err)
	}

	stmts := []string{
		"CREATE EXTENSION IF NOT EXISTS postgis",
		"CREATE EXTENSION IF NOT EXISTS pg_trgm",
		fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", countTestSchema),
		fmt.Sprintf("CREATE SCHEMA %s", countTestSchema),
		// public stays on the path so PostGIS and pg_trgm functions resolve.
		fmt.Sprintf("SET search_path TO %s, public", countTestSchema),
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
		`INSERT INTO ohio_addresses
			(hash, house_number, street, unit, city, district, region, postcode, county, geom, full_address)
		 SELECT
			md5(i::text),
			(100 + i)::text,
			(ARRAY['Barendt Road','Main Street','Oak Avenue','Elm Street','Maple Drive'])[1 + (i % 5)],
			'',
			(ARRAY['Columbus','Cleveland','Cincinnati','Toledo','Akron','Dayton'])[1 + (i % 6)],
			(ARRAY['ADA','FRA','CUY','HAM'])[1 + (i % 4)],
			'OH',
			(43000 + (i % 20))::text,
			(ARRAY['Adams','Franklin','Cuyahoga','Hamilton'])[1 + (i % 4)],
			-- Every row gets a distinct point, on purpose. ORDER BY
			-- ST_Distance(...) has no tiebreaker, so rows at identical
			-- coordinates come back in arbitrary order and a paginated walk
			-- over them is not reproducible. Seeding duplicate points would
			-- make TestPaginationWalkMatchesSingleFetch fail on that
			-- pre-existing instability rather than on anything to do with the
			-- count change it is meant to police.
			ST_SetSRID(ST_MakePoint(-84.0 - i * 0.001, 39.0 + i * 0.001), 4326),
			(100 + i)::text || ' ' ||
			  (ARRAY['Barendt Road','Main Street','Oak Avenue','Elm Street','Maple Drive'])[1 + (i % 5)]
			  || ', ' || (ARRAY['Columbus','Cleveland','Cincinnati','Toledo','Akron','Dayton'])[1 + (i % 6)]
			  || ', OH ' || (43000 + (i % 20))::text
		 FROM generate_series(1, 600) AS i`,
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
		if _, err := db.Exec(fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", countTestSchema)); err != nil {
			t.Logf("cleanup: %v", err)
		}
		db.Close()
	})

	return db
}

// oracle counts rows matching a predicate written out by hand for the case.
func oracle(t *testing.T, db *sql.DB, where string, args ...interface{}) int {
	t.Helper()
	q := "SELECT COUNT(*) FROM ohio_addresses"
	if where != "" {
		q += " WHERE " + where
	}
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("oracle %q: %v", where, err)
	}
	return n
}

// TestSearchTotalMatchesOracle covers every shape that reaches a different
// branch of the count logic: filtered (window count), unfiltered (standalone
// count), relevance scoring, and the distance ORDER BY that renumbers
// placeholders.
func TestSearchTotalMatchesOracle(t *testing.T) {
	db := setupCountTestDB(t)
	svc := NewAddressService(db)

	cases := []struct {
		name        string
		params      models.AddressSearchParams
		whereOracle string
		oracleArgs  []interface{}
	}{
		{
			name:        "unfiltered browse takes the standalone count path",
			params:      models.AddressSearchParams{Limit: 50},
			whereOracle: "",
		},
		{
			name:        "city filter",
			params:      models.AddressSearchParams{City: "Columbus", Limit: 50},
			whereOracle: "city ILIKE $1",
			oracleArgs:  []interface{}{"%Columbus%"},
		},
		{
			// postcode 43005 is i%20==5, whose rows are all i%5==0, so
			// "Barendt" is the street that actually co-occurs with it. Pairing
			// it with a street that never appears there would make this case
			// pass on 0 == 0 and prove nothing.
			name:        "narrow postcode plus street filter",
			params:      models.AddressSearchParams{Postcode: "43005", Street: "Barendt", Limit: 50},
			whereOracle: "postcode = $1 AND street ILIKE $2",
			oracleArgs:  []interface{}{"43005", "%Barendt%"},
		},
		{
			name:        "county filter matching many rows",
			params:      models.AddressSearchParams{County: "Franklin", Limit: 50},
			whereOracle: "county ILIKE $1",
			oracleArgs:  []interface{}{"%Franklin%"},
		},
		{
			name:        "full text query adds relevance score to the select list",
			params:      models.AddressSearchParams{Query: "Barendt", Limit: 50},
			whereOracle: "fts @@ to_tsquery('simple', $1)",
			oracleArgs:  []interface{}{"Barendt:*"},
		},
		{
			name:        "multi word query",
			params:      models.AddressSearchParams{Query: "Barendt Columbus", Limit: 50},
			whereOracle: "fts @@ to_tsquery('simple', $1)",
			oracleArgs:  []interface{}{"Barendt:* & Columbus:*"},
		},
		{
			name:        "query plus filter combines both arg groups",
			params:      models.AddressSearchParams{Query: "Barendt", City: "Columbus", Limit: 50},
			whereOracle: "fts @@ to_tsquery('simple', $1) AND city ILIKE $2",
			oracleArgs:  []interface{}{"Barendt:*", "%Columbus%"},
		},
		{
			// Lat/Lng with no radius contributes ORDER BY parameters only, so
			// the SELECT and WHERE numbering must stay untouched.
			name:        "distance ordering without radius",
			params:      models.AddressSearchParams{Lat: 39.2, Lng: -84.2, Limit: 50},
			whereOracle: "",
		},
		{
			// The maximum-complexity case: WHERE args (text + spatial),
			// SELECT args (relevance), and ORDER BY args (distance) all live.
			name: "query plus radius plus distance ordering",
			params: models.AddressSearchParams{
				Query: "Barendt", Lat: 39.2, Lng: -84.2, Radius: 50, Limit: 50,
			},
			whereOracle: "fts @@ to_tsquery('simple', $1) AND ST_DWithin(geom, ST_SetSRID(ST_MakePoint($2, $3), 4326)::geography, $4)",
			oracleArgs:  []interface{}{"Barendt:*", -84.2, 39.2, 50 * 1000.0},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := oracle(t, db, tc.whereOracle, tc.oracleArgs...)

			rows, total, err := svc.searchAddresses(db, tc.params, false)
			if err != nil {
				t.Fatalf("searchAddresses: %v", err)
			}
			if total != want {
				t.Errorf("total = %d, oracle says %d", total, want)
			}

			// The page itself must still be capped by the limit, and must not
			// have been widened or narrowed by the count change.
			expectRows := want
			if expectRows > tc.params.Limit {
				expectRows = tc.params.Limit
			}
			if len(rows) != expectRows {
				t.Errorf("returned %d rows, expected %d (total %d, limit %d)",
					len(rows), expectRows, want, tc.params.Limit)
			}
			t.Logf("total=%d rows=%d", total, len(rows))
		})
	}
}

// TestTotalIsStableAcrossPages is the offset trap. The window count arrives on
// the rows, so a page past the end returns none and carries no count with it;
// the total must still be right there.
func TestTotalIsStableAcrossPages(t *testing.T) {
	db := setupCountTestDB(t)
	svc := NewAddressService(db)

	for _, tc := range []struct {
		name        string
		base        models.AddressSearchParams
		whereOracle string
		oracleArgs  []interface{}
	}{
		{
			name:        "filtered uses the window count",
			base:        models.AddressSearchParams{City: "Columbus"},
			whereOracle: "city ILIKE $1",
			oracleArgs:  []interface{}{"%Columbus%"},
		},
		{
			name:        "unfiltered uses the standalone count",
			base:        models.AddressSearchParams{},
			whereOracle: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := oracle(t, db, tc.whereOracle, tc.oracleArgs...)

			for _, offset := range []int{0, 10, want - 1, want, want + 500} {
				if offset < 0 {
					continue
				}
				p := tc.base
				p.Limit = 10
				p.Offset = offset

				rows, total, err := svc.searchAddresses(db, p, false)
				if err != nil {
					t.Fatalf("offset %d: %v", offset, err)
				}
				if total != want {
					t.Errorf("offset %d: total = %d, want %d", offset, total, want)
				}
				if offset >= want && len(rows) != 0 {
					t.Errorf("offset %d past end returned %d rows", offset, len(rows))
				}
				t.Logf("offset=%d total=%d rows=%d", offset, total, len(rows))
			}
		})
	}
}

// TestPaginationWalkMatchesSingleFetch proves the rows and their order are
// untouched: walking the result set in small pages must reproduce exactly the
// sequence a single large fetch returns.
func TestPaginationWalkMatchesSingleFetch(t *testing.T) {
	db := setupCountTestDB(t)
	svc := NewAddressService(db)

	for _, tc := range []struct {
		name   string
		params models.AddressSearchParams
	}{
		{"filtered", models.AddressSearchParams{City: "Columbus"}},
		{"unfiltered", models.AddressSearchParams{}},
		{"relevance ordered", models.AddressSearchParams{Query: "Barendt"}},
		{"distance ordered", models.AddressSearchParams{Lat: 39.2, Lng: -84.2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			single := tc.params
			single.Limit = 100
			whole, total, err := svc.searchAddresses(db, single, false)
			if err != nil {
				t.Fatalf("single fetch: %v", err)
			}

			var walked []models.OhioAddress
			for offset := 0; offset < 100; offset += 10 {
				p := tc.params
				p.Limit = 10
				p.Offset = offset
				page, pageTotal, err := svc.searchAddresses(db, p, false)
				if err != nil {
					t.Fatalf("page at %d: %v", offset, err)
				}
				if pageTotal != total {
					t.Errorf("page at %d reported total %d, single fetch said %d",
						offset, pageTotal, total)
				}
				walked = append(walked, page...)
				if len(page) < 10 {
					break
				}
			}

			if len(walked) != len(whole) {
				t.Fatalf("walked %d rows, single fetch returned %d", len(walked), len(whole))
			}
			for i := range whole {
				if whole[i].ID != walked[i].ID {
					t.Fatalf("row %d: single fetch id %d, paginated walk id %d",
						i, whole[i].ID, walked[i].ID)
				}
			}
			t.Logf("total=%d compared %d ids in order", total, len(whole))
		})
	}
}

// TestFuzzyFallbackTotal covers the trigram path, which runs the same builder
// inside a transaction. Because the fallback issues its count on a *sql.Tx, it
// is also the case that would break if the rows were left open.
func TestFuzzyFallbackTotal(t *testing.T) {
	db := setupCountTestDB(t)
	svc := NewAddressService(db)

	// "barrendt" is an insertion typo: the word-prefix tsquery misses it, so
	// SearchAddresses falls through to the trigram pass.
	const typo = "barrendt"

	exact, exactTotal, err := svc.searchAddresses(db, models.AddressSearchParams{Query: typo, Limit: 50}, false)
	if err != nil {
		t.Fatalf("exact pass: %v", err)
	}
	if exactTotal != 0 || len(exact) != 0 {
		t.Fatalf("expected the exact pass to miss %q, got total=%d rows=%d", typo, exactTotal, len(exact))
	}

	// Oracle for the trigram predicate, with the same threshold the service sets.
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := tx.Exec(fmt.Sprintf("SET LOCAL pg_trgm.word_similarity_threshold = %g", fuzzyWordSimilarityThreshold)); err != nil {
		t.Fatalf("set threshold: %v", err)
	}
	var want int
	if err := tx.QueryRow("SELECT COUNT(*) FROM ohio_addresses WHERE $1 <% full_address", typo).Scan(&want); err != nil {
		t.Fatalf("fuzzy oracle: %v", err)
	}
	tx.Commit()

	if want == 0 {
		t.Fatalf("test data does not exercise the fuzzy path: no trigram matches for %q", typo)
	}

	rows, total, err := svc.SearchAddresses(models.AddressSearchParams{Query: typo, Limit: 50})
	if err != nil {
		t.Fatalf("SearchAddresses: %v", err)
	}
	if total != want {
		t.Errorf("fuzzy total = %d, oracle says %d", total, want)
	}
	if len(rows) == 0 {
		t.Error("fuzzy fallback returned no rows")
	}
	t.Logf("fuzzy total=%d rows=%d", total, len(rows))

	// And the offset trap on the fuzzy path, which runs on a transaction.
	_, pastEnd, err := svc.SearchAddresses(models.AddressSearchParams{Query: typo, Limit: 10, Offset: want + 100})
	if err != nil {
		t.Fatalf("fuzzy past end: %v", err)
	}
	if pastEnd != want {
		t.Errorf("fuzzy total past end = %d, want %d", pastEnd, want)
	}
}
