package services

import (
	"database/sql"
	"fmt"
	"os"
	"testing"

	"geocoding-api/models"

	_ "github.com/lib/pq"
)

const multiStateSchema = "multistate_probe"

// setupMultiStateDB builds the table with the post-migration-23 uniqueness key.
func setupMultiStateDB(t *testing.T, keyedOnRegion bool) *sql.DB {
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

	unique := "hash VARCHAR(255) UNIQUE NOT NULL"
	if keyedOnRegion {
		unique = "hash VARCHAR(255) NOT NULL"
	}

	stmts := []string{
		"CREATE EXTENSION IF NOT EXISTS postgis",
		"CREATE EXTENSION IF NOT EXISTS pg_trgm",
		fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", multiStateSchema),
		fmt.Sprintf("CREATE SCHEMA %s", multiStateSchema),
		fmt.Sprintf("SET search_path TO %s, public", multiStateSchema),
		fmt.Sprintf(`CREATE TABLE ohio_addresses (
			id BIGSERIAL PRIMARY KEY,
			%s,
			house_number VARCHAR(50), street VARCHAR(255), unit VARCHAR(50),
			city VARCHAR(255), district VARCHAR(10), region VARCHAR(2) NOT NULL,
			postcode VARCHAR(10), county VARCHAR(255),
			geom GEOMETRY(POINT, 4326) NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			full_address TEXT
		)`, unique),
	}
	if keyedOnRegion {
		stmts = append(stmts, "CREATE UNIQUE INDEX ON ohio_addresses (hash, region)",
			"ALTER TABLE ohio_addresses ADD CONSTRAINT ohio_addresses_region_not_blank CHECK (region <> '')")
	}
	stmts = append(stmts,
		"CREATE INDEX ON ohio_addresses (region)",
		// Text search runs against the generated tsvector, so a fixture without
		// it cannot exercise a query combined with the territory filters.
		`ALTER TABLE ohio_addresses ADD COLUMN fts tsvector
			GENERATED ALWAYS AS (to_tsvector('simple', coalesce(full_address, ''))) STORED`,
		"CREATE INDEX ON ohio_addresses USING gin (fts)",
		"CREATE INDEX ON ohio_addresses USING gin (full_address gin_trgm_ops)",
	)

	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("setup failed on %.60q: %v", stmt, err)
		}
	}

	t.Cleanup(func() {
		if _, err := db.Exec(fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", multiStateSchema)); err != nil {
			t.Logf("cleanup: %v", err)
		}
		db.Close()
	})

	return db
}

// insertAddress mirrors what the importer writes, including the hash it builds.
func insertAddress(db *sql.DB, house, street, unit, city, postcode, county, region string) (int64, error) {
	hash := fmt.Sprintf("%s|%s|%s|%s|%s", house, street, unit, city, postcode)
	var id int64
	err := db.QueryRow(`
		INSERT INTO ohio_addresses (hash, house_number, street, unit, city, district, region, postcode, county, geom, full_address)
		VALUES ($1,$2,$3,$4,$5,'',$6,$7,$8, ST_SetSRID(ST_MakePoint(-83.0, 40.0), 4326), $9)
		ON CONFLICT (hash, region) DO NOTHING
		RETURNING id
	`, hash, house, street, unit, city, region, postcode, county,
		fmt.Sprintf("%s %s, %s, %s %s", house, street, city, region, postcode)).Scan(&id)
	return id, err
}

// The blocker, demonstrated. Two states, the same street address, no ZIP --
// which ingestion permits, since it requires only a house number and a street,
// and county GeoJSON extracts frequently ship without a ZIP column.
func TestSecondStateSurvivesWithoutAPostcode(t *testing.T) {
	db := setupMultiStateDB(t, true)

	if _, err := insertAddress(db, "100", "Main Street", "", "Springfield", "", "Clark", "OH"); err != nil {
		t.Fatalf("insert Ohio row: %v", err)
	}
	if _, err := insertAddress(db, "100", "Main Street", "", "Springfield", "", "Sangamon", "IL"); err != nil {
		if err == sql.ErrNoRows {
			t.Fatal("the Illinois row was silently discarded as a duplicate of the Ohio one")
		}
		t.Fatalf("insert Illinois row: %v", err)
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ohio_addresses`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Errorf("%d row(s) survived, want 2 -- a state's data is being dropped", n)
	}
}

// The old key, so the regression this closes is on the record. Under a global
// unique hash the second state's row vanishes and the importer reports it as a
// duplicate, so a load that lost half a state completes green.
func TestGlobalHashDropsTheSecondState(t *testing.T) {
	db := setupMultiStateDB(t, false)

	hash := "100|Main Street||Springfield|"
	insert := func(region, county string) error {
		_, err := db.Exec(`
			INSERT INTO ohio_addresses (hash, house_number, street, unit, city, district, region, postcode, county, geom)
			VALUES ($1,'100','Main Street','','Springfield','',$2,'',$3, ST_SetSRID(ST_MakePoint(-83.0,40.0),4326))
			ON CONFLICT (hash) DO NOTHING
		`, hash, region, county)
		return err
	}

	if err := insert("OH", "Clark"); err != nil {
		t.Fatalf("ohio: %v", err)
	}
	if err := insert("IL", "Sangamon"); err != nil {
		t.Fatalf("illinois: %v", err)
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ohio_addresses`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected the old key to drop the second state, got %d rows", n)
	}

	var region string
	if err := db.QueryRow(`SELECT region FROM ohio_addresses`).Scan(&region); err != nil {
		t.Fatalf("survivor: %v", err)
	}
	t.Logf("under the old key only %s survived; Illinois was counted as a duplicate", region)
}

// Duplicates within one state must still be rejected -- widening the key must
// not turn off deduplication.
func TestDuplicatesWithinAStateAreStillRejected(t *testing.T) {
	db := setupMultiStateDB(t, true)

	if _, err := insertAddress(db, "100", "Main Street", "", "Springfield", "", "Clark", "OH"); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	_, err := insertAddress(db, "100", "Main Street", "", "Springfield", "", "Clark", "OH")
	if err != sql.ErrNoRows {
		t.Errorf("a genuine duplicate was accepted (err=%v); dedup is off", err)
	}

	var n int
	db.QueryRow(`SELECT COUNT(*) FROM ohio_addresses`).Scan(&n)
	if n != 1 {
		t.Errorf("%d rows after inserting the same address twice, want 1", n)
	}
}

// County names collide across states, so a county filter without a state is
// ambiguous the moment a second state exists.
func TestStateFilterSeparatesCollidingCounties(t *testing.T) {
	db := setupMultiStateDB(t, true)
	svc := NewAddressService(db)

	for _, row := range []struct{ house, city, county, region string }{
		{"1", "Columbus", "Franklin", "OH"},
		{"2", "Columbus", "Franklin", "OH"},
		{"3", "Winchester", "Franklin", "IN"},
	} {
		if _, err := insertAddress(db, row.house, "Main Street", "", row.city, "", row.county, row.region); err != nil {
			t.Fatalf("seed %s/%s: %v", row.region, row.county, err)
		}
	}

	_, bothStates, err := svc.SearchAddresses(models.AddressSearchParams{County: "Franklin", Limit: 50})
	if err != nil {
		t.Fatalf("county only: %v", err)
	}
	if bothStates != 3 {
		t.Errorf("county filter alone returned %d, want 3 -- it should still match across states", bothStates)
	}

	rows, ohioOnly, err := svc.SearchAddresses(models.AddressSearchParams{
		County: "Franklin", State: "OH", Limit: 50,
	})
	if err != nil {
		t.Fatalf("county + state: %v", err)
	}
	if ohioOnly != 2 {
		t.Errorf("Franklin County, OH returned %d, want 2", ohioOnly)
	}
	for _, r := range rows {
		if r.Region != "OH" {
			t.Errorf("%s is in %s but the filter asked for OH", r.FullAddress, r.Region)
		}
	}
	t.Logf("Franklin County: %d across states, %d in Ohio", bothStates, ohioOnly)
}

// A caller passing a lowercase code should not be told there is no data.
func TestStateFilterIsCaseInsensitive(t *testing.T) {
	db := setupMultiStateDB(t, true)
	svc := NewAddressService(db)

	if _, err := insertAddress(db, "1", "Main Street", "", "Columbus", "", "Franklin", "OH"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	for _, code := range []string{"OH", "oh", " oh "} {
		_, total, err := svc.SearchAddresses(models.AddressSearchParams{State: code, Limit: 10})
		if err != nil {
			t.Fatalf("state %q: %v", code, err)
		}
		if total != 1 {
			t.Errorf("state %q returned %d rows, want 1", code, total)
		}
	}
}

// The geocoding path parsed a state out of the query and threw it away. Once a
// second state is loaded, "100 Main St, Springfield, IL" matched the Ohio row
// on street and city and came back at full confidence -- the exact collision
// this whole change exists to prevent, on the endpoint that matters most.
func TestGeocodingRespectsTheStateInTheQuery(t *testing.T) {
	db := setupMultiStateDB(t, true)
	svc := NewAddressService(db)

	if _, err := insertAddress(db, "100", "Main Street", "", "Springfield", "45503", "Clark", "OH"); err != nil {
		t.Fatalf("seed OH: %v", err)
	}
	if _, err := insertAddress(db, "100", "Main Street", "", "Springfield", "62701", "Sangamon", "IL"); err != nil {
		t.Fatalf("seed IL: %v", err)
	}

	result, err := svc.FullTextSearchAddresses("100 Main St, Springfield, IL", 10)
	if err != nil {
		t.Fatalf("geocode: %v", err)
	}
	if len(result.Addresses) == 0 {
		t.Fatal("no match for an address that exists")
	}
	for _, a := range result.Addresses {
		if a.Region != "IL" {
			t.Errorf("query named IL but %s in %s came back", a.FullAddress, a.Region)
		}
	}
	t.Logf("query named IL, got %d row(s), all in IL", len(result.Addresses))

	// And the same query for Ohio must return the Ohio row, not the Illinois
	// one -- the filter has to select, not merely exclude.
	result, err = svc.FullTextSearchAddresses("100 Main St, Springfield, OH", 10)
	if err != nil {
		t.Fatalf("geocode OH: %v", err)
	}
	for _, a := range result.Addresses {
		if a.Region != "OH" {
			t.Errorf("query named OH but %s in %s came back", a.FullAddress, a.Region)
		}
	}
}

// A blank region would put every stateless row into one uniqueness bucket,
// reintroducing the collision for any dataset uploaded without a state.
func TestBlankRegionIsRejected(t *testing.T) {
	db := setupMultiStateDB(t, true)

	_, err := db.Exec(`
		INSERT INTO ohio_addresses (hash, house_number, street, unit, city, district, region, postcode, county, geom)
		VALUES ('blank','1','Main Street','','Columbus','','','43004','Franklin', ST_SetSRID(ST_MakePoint(-83.0,40.0),4326))
	`)
	if err == nil {
		t.Error("an empty region was accepted; every stateless row would share one uniqueness bucket")
	}
}

// State and territory filters arrived on separate branches and first met in a
// merge. They share the hand-numbered placeholder sequence -- the bbox consumes
// four positions -- so a mistake there binds the wrong value to the wrong
// column and returns plausible rows from the wrong place.
func TestStateAndBBoxComposeCorrectly(t *testing.T) {
	db := setupMultiStateDB(t, true)
	svc := NewAddressService(db)

	// Same coordinates, two states, so only the state filter can separate them.
	for _, row := range []struct{ house, county, region string }{
		{"1", "Franklin", "OH"},
		{"2", "Franklin", "OH"},
		{"3", "Sangamon", "IL"},
	} {
		if _, err := insertAddress(db, row.house, "Main Street", "", "Springfield", "", row.county, row.region); err != nil {
			t.Fatalf("seed %s: %v", row.region, err)
		}
	}

	box := &models.BoundingBox{MinLng: -83.1, MinLat: 39.9, MaxLng: -82.9, MaxLat: 40.1}

	_, boxOnly, err := svc.SearchAddresses(models.AddressSearchParams{BBox: box, Limit: 50})
	if err != nil {
		t.Fatalf("bbox only: %v", err)
	}
	if boxOnly != 3 {
		t.Fatalf("bbox alone returned %d, want all 3 seeded rows", boxOnly)
	}

	rows, combined, err := svc.SearchAddresses(models.AddressSearchParams{
		BBox: box, State: "OH", Limit: 50,
	})
	if err != nil {
		t.Fatalf("bbox + state: %v", err)
	}
	if combined != 2 {
		t.Errorf("bbox + state=OH returned %d, want 2; the placeholders may be misaligned", combined)
	}
	for _, r := range rows {
		if r.Region != "OH" {
			t.Errorf("%s is in %s despite state=OH", r.FullAddress, r.Region)
		}
	}

	// And with a text query on top, which adds placeholders of its own.
	_, withQuery, err := svc.SearchAddresses(models.AddressSearchParams{
		BBox: box, State: "OH", Query: "Main", Limit: 50,
	})
	if err != nil {
		t.Fatalf("bbox + state + query: %v", err)
	}
	if withQuery != 2 {
		t.Errorf("bbox + state + query returned %d, want 2", withQuery)
	}
	t.Logf("bbox %d, +state %d, +query %d", boxOnly, combined, withQuery)
}
