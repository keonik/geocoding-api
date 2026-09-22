package services

import (
	"database/sql"
	"fmt"
	"os"
	"testing"

	"geocoding-api/models"

	_ "github.com/lib/pq"
)

const nullColumnsSchema = "null_columns_probe"

// Every text column on ohio_addresses but region is nullable, and county
// extracts without a ZIP column are ordinary. A row like that used to fail
// the scan -- "converting NULL to string is unsupported" -- and take the
// whole response with it, so one address with no postcode turned every
// search that matched it into a 500.
func setupNullColumnsDB(t *testing.T) *sql.DB {
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
	t.Cleanup(func() {
		if _, err := db.Exec("DROP SCHEMA IF EXISTS " + nullColumnsSchema + " CASCADE"); err != nil {
			t.Logf("cleanup: %v", err)
		}
		db.Close()
	})

	for _, stmt := range []string{
		"CREATE EXTENSION IF NOT EXISTS postgis",
		"CREATE EXTENSION IF NOT EXISTS pg_trgm",
		"DROP SCHEMA IF EXISTS " + nullColumnsSchema + " CASCADE",
		"CREATE SCHEMA " + nullColumnsSchema,
		fmt.Sprintf("SET search_path TO %s, public", nullColumnsSchema),
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
		"CREATE INDEX ON ohio_addresses USING gist (geom)",
		// One ordinary row, and one with every nullable column null -- what a
		// county extract without ZIPs or unit numbers actually loads as.
		`INSERT INTO ohio_addresses (hash, house_number, street, unit, city, district, region, postcode, county, geom, full_address) VALUES
		 ('complete','1','Main Street','','Columbus','FRA','OH','43215','Franklin',
		  ST_SetSRID(ST_MakePoint(-83.0007,39.9612),4326),'1 Main Street, Columbus, OH 43215'),
		 ('sparse', NULL, 'Main Street', NULL, NULL, NULL, 'OH', NULL, NULL,
		  ST_SetSRID(ST_MakePoint(-83.0010,39.9615),4326),'2 Main Street')`,
		// Reverse reads these too. Created here, empty, so nothing resolves
		// through public and answers from a real deployment's data.
		`CREATE TABLE ohio_counties (
			id BIGSERIAL PRIMARY KEY, county_name VARCHAR(255) NOT NULL,
			bounds_geometry GEOMETRY(MULTIPOLYGON, 4326))`,
		`CREATE TABLE us_states (
			id BIGSERIAL PRIMARY KEY, state_fips VARCHAR(2) NOT NULL UNIQUE,
			state_abbr VARCHAR(2) NOT NULL UNIQUE, state_name VARCHAR(255) NOT NULL UNIQUE,
			geometry GEOMETRY(MULTIPOLYGON, 4326))`,
		`CREATE TABLE zip_codes (
			zip_code VARCHAR(10) PRIMARY KEY, city_name VARCHAR(255), state_code VARCHAR(2),
			timezone VARCHAR(100) NOT NULL DEFAULT '', latitude DOUBLE PRECISION, longitude DOUBLE PRECISION,
			geog geography(Point,4326) GENERATED ALWAYS AS
				(ST_SetSRID(ST_MakePoint(longitude, latitude), 4326)::geography) STORED)`,
		"ANALYZE ohio_addresses",
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("setup failed on %.60q: %v", stmt, err)
		}
	}
	createBoundaryTables(t, db)
	return db
}

func TestNullColumnsDoNotBreakAddressReads(t *testing.T) {
	db := setupNullColumnsDB(t)
	svc := NewAddressService(db)

	t.Run("search", func(t *testing.T) {
		found, total, err := svc.SearchAddresses(models.AddressSearchParams{Query: "main street", Limit: 10})
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if total < 2 || len(found) < 2 {
			t.Fatalf("found %d of 2 rows", len(found))
		}
		for _, a := range found {
			if a.Street != "Main Street" {
				t.Errorf("street = %q", a.Street)
			}
		}
	})

	t.Run("full text", func(t *testing.T) {
		result, err := svc.FullTextSearchAddresses("Main Street", 10)
		if err != nil {
			t.Fatalf("full-text: %v", err)
		}
		if len(result.Addresses) < 2 {
			t.Errorf("found %d of 2 rows via %s", len(result.Addresses), result.SearchMethod)
		}
	})

	t.Run("prefix", func(t *testing.T) {
		// The typeahead path: a partial word, which is what a keystroke is.
		result, err := svc.FullTextSearchAddresses("Mai", 10)
		if err != nil {
			t.Fatalf("prefix search: %v", err)
		}
		if len(result.Addresses) == 0 {
			t.Error("a partial word matched nothing")
		}
	})

	t.Run("by id", func(t *testing.T) {
		var id int64
		if err := db.QueryRow(`SELECT id FROM ohio_addresses WHERE hash = 'sparse'`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		addr, err := svc.GetAddressByID(id)
		if err != nil {
			t.Fatalf("by id: %v", err)
		}
		// The absent fields read as empty, which is what an absent field
		// means everywhere else in this API.
		if addr.Postcode != "" || addr.City != "" || addr.Unit != "" || addr.County != "" || addr.HouseNumber != "" {
			t.Errorf("nulls did not read as empty: %+v", addr)
		}
		if addr.Region != "OH" || addr.Street != "Main Street" {
			t.Errorf("present fields were lost: %+v", addr)
		}
	})

	t.Run("reverse", func(t *testing.T) {
		got, err := ReverseGeocode(db, 39.9615, -83.0010, 0)
		if err != nil {
			t.Fatalf("reverse: %v", err)
		}
		if got.Address == nil {
			t.Fatal("no address found beside one")
		}
	})

	t.Run("validate", func(t *testing.T) {
		if _, err := ValidateAddress(db, "2 Main Street"); err != nil {
			t.Fatalf("validate: %v", err)
		}
	})

	t.Run("batch", func(t *testing.T) {
		resp, err := BatchGeocode(db, []BatchItem{{Query: "Main Street"}})
		if err != nil {
			t.Fatalf("batch: %v", err)
		}
		if resp.Results[0].Error != "" {
			t.Errorf("batch item failed: %s", resp.Results[0].Error)
		}
	})
}
