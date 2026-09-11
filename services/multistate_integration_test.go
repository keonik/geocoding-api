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
		stmts = append(stmts, "CREATE UNIQUE INDEX ON ohio_addresses (hash, region)")
	}
	stmts = append(stmts, "CREATE INDEX ON ohio_addresses (region)")

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
