package services

import (
	"database/sql"
	"fmt"
	"os"
	"testing"

	_ "github.com/lib/pq"
)

const batchSchema = "batch_probe"

// setupBatchDB uses the real zip_codes column set from database/migrations.go,
// not a trimmed one: the batch query selects every column the single-ZIP
// endpoint does, so a fixture missing any of them tests a different query than
// production runs.
func setupBatchDB(t *testing.T) *sql.DB {
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

	stmts := []string{
		"CREATE EXTENSION IF NOT EXISTS postgis",
		"CREATE EXTENSION IF NOT EXISTS pg_trgm",
		fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", batchSchema),
		fmt.Sprintf("CREATE SCHEMA %s", batchSchema),
		fmt.Sprintf("SET search_path TO %s, public", batchSchema),
		`CREATE TABLE zip_codes (
			zip_code VARCHAR(10) PRIMARY KEY,
			city_name VARCHAR(255) NOT NULL,
			state_code VARCHAR(2) NOT NULL,
			state_name VARCHAR(255) NOT NULL,
			zcta BOOLEAN NOT NULL DEFAULT FALSE,
			zcta_parent VARCHAR(10),
			population DECIMAL(12,2),
			density DECIMAL(10,2),
			primary_county_code VARCHAR(10) NOT NULL,
			primary_county_name VARCHAR(255) NOT NULL,
			county_weights JSONB,
			county_names TEXT,
			county_codes TEXT,
			imprecise BOOLEAN NOT NULL DEFAULT FALSE,
			military BOOLEAN NOT NULL DEFAULT FALSE,
			timezone VARCHAR(100) NOT NULL,
			latitude DECIMAL(10,7) NOT NULL,
			longitude DECIMAL(10,7) NOT NULL
		)`,
		`INSERT INTO zip_codes (zip_code, city_name, state_code, state_name, zcta, zcta_parent,
			population, density, primary_county_code, primary_county_name, county_weights,
			county_names, county_codes, imprecise, military, timezone, latitude, longitude) VALUES
		 ('43215','Columbus','OH','Ohio',true,'',1000,10.0,'39049','Franklin','{}'::jsonb,'Franklin','39049',
		  false,false,'America/New_York',39.9612,-83.0007),
		 ('43617','Toledo','OH','Ohio',true,'',1000,10.0,'39095','Lucas','{}'::jsonb,'Lucas','39095',
		  false,false,'America/New_York',41.6600,-83.6000)`,
		`CREATE TABLE ohio_addresses (
			id BIGSERIAL PRIMARY KEY, hash VARCHAR(255) NOT NULL,
			house_number VARCHAR(50), street VARCHAR(255), unit VARCHAR(50),
			city VARCHAR(255), district VARCHAR(10), region VARCHAR(2) NOT NULL,
			postcode VARCHAR(10), county VARCHAR(255),
			geom GEOMETRY(POINT, 4326) NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP, full_address TEXT)`,
		`ALTER TABLE ohio_addresses ADD COLUMN fts tsvector
			GENERATED ALWAYS AS (to_tsvector('simple', coalesce(full_address, ''))) STORED`,
		"CREATE INDEX ON ohio_addresses USING gin (fts)",
		"CREATE INDEX ON ohio_addresses USING gin (full_address gin_trgm_ops)",
		"CREATE INDEX ON ohio_addresses (county)",
		`INSERT INTO ohio_addresses (hash, house_number, street, unit, city, district, region, postcode, county, geom, full_address)
		 VALUES ('b1','100','Main Street','','Columbus','FRA','OH','43215','Franklin',
		         ST_SetSRID(ST_MakePoint(-83.0007,39.9612),4326),'100 Main Street, Columbus, OH 43215')`,
		"ANALYZE zip_codes",
		"ANALYZE ohio_addresses",
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("setup failed on %.60q: %v", stmt, err)
		}
	}

	t.Cleanup(func() {
		if _, err := db.Exec(fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", batchSchema)); err != nil {
			t.Logf("cleanup: %v", err)
		}
		db.Close()
	})
	return db
}

// The batch endpoint exists to save round trips. Issuing one query per item
// internally would give that straight back.
func TestBatchResolvesZipsInOneQuery(t *testing.T) {
	db := setupBatchDB(t)

	items := []BatchItem{
		{ID: "a", ZipCode: "43215"},
		{ID: "b", ZipCode: "43617"},
		{ID: "c", ZipCode: "00000"}, // no such ZIP
		{ID: "d", ZipCode: "43215"}, // repeated
	}

	resp, err := BatchGeocode(db, items)
	if err != nil {
		t.Fatalf("batch: %v", err)
	}

	if resp.Total != 4 {
		t.Errorf("total = %d, want 4", resp.Total)
	}
	if resp.Found != 3 {
		t.Errorf("found = %d, want 3 (the unknown ZIP is a miss, not an error)", resp.Found)
	}
	// Results must line up with the items that produced them.
	for i, want := range []string{"a", "b", "c", "d"} {
		if resp.Results[i].ID != want {
			t.Errorf("result %d has id %q, want %q -- results are out of order", i, resp.Results[i].ID, want)
		}
	}
	if resp.Results[2].Found {
		t.Error("an unknown ZIP was reported as found")
	}
	if resp.Results[2].Error != "" {
		t.Errorf("an unknown ZIP produced an error %q; a miss is not a failure", resp.Results[2].Error)
	}
	// A repeated ZIP is one row from the database and an answer in both places.
	if !resp.Results[3].Found || resp.Results[3].ZipCode == nil {
		t.Error("a ZIP repeated in the batch was not answered the second time")
	}
}

// Billing is the part that is easy to get wrong and expensive when it is: a
// batch of 100 that counts as one call under-bills by 100x and lets a caller
// walk past their own limit by wrapping every lookup in a batch.
func TestBatchBillsPerItemNotPerRequest(t *testing.T) {
	db := setupBatchDB(t)

	resp, err := BatchGeocode(db, []BatchItem{
		{ZipCode: "43215"}, {ZipCode: "43617"}, {ZipCode: "00000"},
	})
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	if resp.BillableUnits != 3 {
		t.Errorf("billable units = %d, want 3 -- one per item, including misses", resp.BillableUnits)
	}
}

// An item that names neither, or both, is a per-item error rather than a failed
// request: one malformed entry must not discard ninety-nine good answers.
func TestBatchReportsPerItemErrors(t *testing.T) {
	db := setupBatchDB(t)

	resp, err := BatchGeocode(db, []BatchItem{
		{ID: "good", ZipCode: "43215"},
		{ID: "neither"},
		{ID: "both", ZipCode: "43215", Query: "Main Street"},
	})
	if err != nil {
		t.Fatalf("batch should not fail wholesale: %v", err)
	}

	if !resp.Results[0].Found {
		t.Error("the valid item was not answered")
	}
	if resp.Results[1].Error == "" {
		t.Error("an item with neither zip_code nor query produced no error")
	}
	if resp.Results[2].Error == "" {
		t.Error("an item with both zip_code and query produced no error")
	}
	// Still billed: the work of validating and rejecting them was performed.
	if resp.BillableUnits != 3 {
		t.Errorf("billable units = %d, want 3", resp.BillableUnits)
	}
}

// An unbounded batch is an unbounded query.
func TestBatchRejectsOversizedRequests(t *testing.T) {
	db := setupBatchDB(t)

	items := make([]BatchItem, MaxBatchItems+1)
	for i := range items {
		items[i] = BatchItem{ZipCode: "43215"}
	}
	if _, err := BatchGeocode(db, items); err == nil {
		t.Errorf("a batch of %d was accepted; the cap is %d", len(items), MaxBatchItems)
	}

	if _, err := BatchGeocode(db, nil); err == nil {
		t.Error("an empty batch was accepted")
	}
}

// Address lookups come back in the right slots alongside ZIP lookups.
func TestBatchMixesZipAndAddressLookups(t *testing.T) {
	db := setupBatchDB(t)

	resp, err := BatchGeocode(db, []BatchItem{
		{ID: "zip", ZipCode: "43215"},
		{ID: "addr", Query: "Main Street"},
	})
	if err != nil {
		t.Fatalf("batch: %v", err)
	}

	if resp.Results[0].ZipCode == nil {
		t.Error("the ZIP item did not return a ZIP")
	}
	if resp.Results[0].Address != nil {
		t.Error("the ZIP item returned an address")
	}
	if resp.Results[1].Address == nil {
		t.Error("the address item did not return an address")
	}
	if resp.Results[1].ZipCode != nil {
		t.Error("the address item returned a ZIP")
	}
}
