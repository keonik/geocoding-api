package database

import (
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

// Pins migration 21, which puts a geography point and GIST index on zip_codes
// so radius search can use ST_DWithin instead of a bounding box.
//
// The properties that matter are that it is re-runnable (migrations here run
// on every boot against a database that may already have them), that Down
// undoes it, and above all that the column is GENERATED rather than backfilled
// -- ZIP data is reloaded from CSV by InitializeData and by the admin
// /load-data endpoint, and a backfilled column would silently go stale on
// every reload.
//
// Skipped unless PROBE_DSN is set.
func TestZipCodeGeographyMigration(t *testing.T) {
	dsn := os.Getenv("PROBE_DSN")
	if dsn == "" {
		t.Skip("PROBE_DSN not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// Closed in the t.Cleanup below, not by a defer here: deferred calls run
	// when the test function returns, which is *before* registered cleanups,
	// so a `defer db.Close()` would shut the pool while the cleanup still
	// needs it and the schema would leak.

	// A private schema, not public: `go test ./...` runs packages in parallel
	// and the services package builds its own zip_codes fixture against the
	// same PROBE_DSN. search_path is per-session, so pin the pool to one
	// connection to make it stick.
	db.SetMaxOpenConns(1)
	schema := fmt.Sprintf("fixture_%d", time.Now().UnixNano())
	if _, err := db.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatalf("create fixture schema: %v", err)
	}
	if _, err := db.Exec(`SET search_path TO ` + schema + `, public`); err != nil {
		t.Fatalf("set search_path: %v", err)
	}

	prev := DB
	DB = db
	t.Cleanup(func() {
		DB = prev
		if _, err := db.Exec(`DROP SCHEMA IF EXISTS ` + schema + ` CASCADE`); err != nil {
			t.Errorf("fixture schema %s leaked: %v", schema, err)
		}
		db.Close()
	})

	if err := createZipCodesTable(); err != nil {
		t.Fatalf("createZipCodesTable: %v", err)
	}

	_, err = db.Exec(`
		INSERT INTO zip_codes (zip_code, city_name, state_code, state_name,
			primary_county_code, primary_county_name, timezone, latitude, longitude)
		VALUES ('43215', 'Columbus', 'OH', 'Ohio', '39049', 'Franklin',
			'America/New_York', 39.9612, -83.0007)
	`)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := addZipCodeGeography(); err != nil {
		t.Fatalf("addZipCodeGeography: %v", err)
	}

	// Re-running must be a no-op, not an error: migrations run on every boot.
	if err := addZipCodeGeography(); err != nil {
		t.Fatalf("addZipCodeGeography is not re-runnable: %v", err)
	}

	var wkt string
	if err := db.QueryRow(`SELECT ST_AsText(geog::geometry) FROM zip_codes WHERE zip_code = '43215'`).Scan(&wkt); err != nil {
		t.Fatalf("read geog: %v", err)
	}
	if wkt != "POINT(-83.0007 39.9612)" {
		t.Errorf("geog is %q, want POINT(-83.0007 39.9612) -- lng/lat order matters", wkt)
	}

	var isGenerated string
	err = db.QueryRow(`
		SELECT is_generated FROM information_schema.columns
		WHERE table_name = 'zip_codes' AND column_name = 'geog'
	`).Scan(&isGenerated)
	if err != nil {
		t.Fatalf("read column metadata: %v", err)
	}
	if isGenerated != "ALWAYS" {
		t.Errorf("geog is_generated=%q, want ALWAYS -- a backfilled column goes stale when ZIP data is reloaded", isGenerated)
	}

	// The staleness property, exercised rather than asserted: the upsert in
	// LoadZipCodesFromCSV updates latitude/longitude and never mentions geog.
	_, err = db.Exec(`
		INSERT INTO zip_codes (zip_code, city_name, state_code, state_name,
			primary_county_code, primary_county_name, timezone, latitude, longitude)
		VALUES ('43215', 'Columbus', 'OH', 'Ohio', '39049', 'Franklin',
			'America/New_York', 41.4993, -81.6944)
		ON CONFLICT (zip_code) DO UPDATE SET
			latitude = EXCLUDED.latitude,
			longitude = EXCLUDED.longitude
	`)
	if err != nil {
		t.Fatalf("reload upsert: %v", err)
	}
	if err := db.QueryRow(`SELECT ST_AsText(geog::geometry) FROM zip_codes WHERE zip_code = '43215'`).Scan(&wkt); err != nil {
		t.Fatalf("read geog after reload: %v", err)
	}
	if wkt != "POINT(-81.6944 41.4993)" {
		t.Errorf("geog is %q after a lat/lng reload, want POINT(-81.6944 41.4993)", wkt)
	}

	var hasIndex bool
	if err := db.QueryRow(`SELECT to_regclass('idx_zip_codes_geog') IS NOT NULL`).Scan(&hasIndex); err != nil {
		t.Fatalf("check index: %v", err)
	}
	if !hasIndex {
		t.Error("idx_zip_codes_geog was not created")
	}

	if err := removeZipCodeGeography(); err != nil {
		t.Fatalf("removeZipCodeGeography: %v", err)
	}

	var hasColumn bool
	if err := db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name = 'zip_codes' AND column_name = 'geog'
		)
	`).Scan(&hasColumn); err != nil {
		t.Fatalf("check column after down: %v", err)
	}
	if hasColumn {
		t.Error("geog column survived removeZipCodeGeography")
	}
	if err := db.QueryRow(`SELECT to_regclass('idx_zip_codes_geog') IS NOT NULL`).Scan(&hasIndex); err != nil {
		t.Fatalf("check index after down: %v", err)
	}
	if hasIndex {
		t.Error("idx_zip_codes_geog survived removeZipCodeGeography")
	}
}
