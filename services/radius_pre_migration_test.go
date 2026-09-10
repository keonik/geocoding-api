package services

import (
	"database/sql"
	"fmt"
	"os"
	"testing"

	"geocoding-api/database"

	_ "github.com/lib/pq"
)

const preMigrationSchema = "radius_premigration_probe"

// Reproduces the window between a deploy and migration 21: the server is
// serving, zip_codes exists, but the geography column does not yet. Migrations
// run asynchronously by default (main.go), so this state is reachable in
// production on every deploy that introduces the column.
func setupPreMigrationZips(t *testing.T, withGeog bool) *sql.DB {
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
		fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", preMigrationSchema),
		fmt.Sprintf("CREATE SCHEMA %s", preMigrationSchema),
		fmt.Sprintf("SET search_path TO %s, public", preMigrationSchema),
		// Copied from database/migrations.go so the fixture cannot drift from
		// the real column types -- county_weights being JSONB rather than TEXT
		// matters to how models.ZipCode scans.
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
			longitude DECIMAL(10,7) NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		// Columbus OH and four neighbours a few miles out.
		// Every column is populated: models.ZipCode scans several of these into
		// non-pointer types, so a NULL is a scan error rather than a zero.
		`INSERT INTO zip_codes (zip_code, city_name, state_code, state_name, zcta,
			zcta_parent, population, density, primary_county_code, primary_county_name,
			county_weights, county_names, county_codes, imprecise, military, timezone,
			latitude, longitude) VALUES
			('43215','Columbus','OH','Ohio',true,'',1000,10.0,'39049','Franklin','{}'::jsonb,'Franklin','39049',false,false,'America/New_York',39.9612,-83.0007),
			('43201','Columbus','OH','Ohio',true,'',1000,10.0,'39049','Franklin','{}'::jsonb,'Franklin','39049',false,false,'America/New_York',39.9878,-83.0044),
			('43202','Columbus','OH','Ohio',true,'',1000,10.0,'39049','Franklin','{}'::jsonb,'Franklin','39049',false,false,'America/New_York',40.0198,-83.0180),
			('43203','Columbus','OH','Ohio',true,'',1000,10.0,'39049','Franklin','{}'::jsonb,'Franklin','39049',false,false,'America/New_York',39.9750,-82.9640),
			('90210','Beverly Hills','CA','California',true,'',1000,10.0,'06037','Los Angeles','{}'::jsonb,'Los Angeles','06037',false,false,'America/Los_Angeles',34.0901,-118.4065)`,
	}
	if withGeog {
		stmts = append(stmts,
			`ALTER TABLE zip_codes ADD COLUMN geog geography(Point,4326)
				GENERATED ALWAYS AS (
					ST_SetSRID(ST_MakePoint(longitude::double precision, latitude::double precision), 4326)::geography
				) STORED`,
			"CREATE INDEX ON zip_codes USING GIST (geog)",
		)
	}
	stmts = append(stmts, "ANALYZE zip_codes")

	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("setup failed on %.60q: %v", stmt, err)
		}
	}

	prev := database.DB
	database.DB = db
	t.Cleanup(func() {
		database.DB = prev
		if _, err := db.Exec(fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", preMigrationSchema)); err != nil {
			t.Logf("cleanup: %v", err)
		}
		db.Close()
	})

	return db
}

// Without the fallback this returns `column z.geog does not exist` and the
// endpoint 500s for the whole migration window.
func TestRadiusSearchWorksBeforeGeographyMigration(t *testing.T) {
	setupPreMigrationZips(t, false)

	results, err := FindZipCodesWithinRadius("43215", 10, 25)
	if err != nil {
		t.Fatalf("radius search before migration 21: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected neighbouring ZIPs, got none")
	}
	for _, r := range results {
		if r.ZipCode.ZipCode == "90210" {
			t.Error("Beverly Hills is not within 10 miles of Columbus")
		}
	}
	t.Logf("pre-migration fallback returned %d ZIPs", len(results))
}

// The fallback must agree with the indexed path exactly -- same rows, same
// order, same distances. If it did not, the migration landing mid-flight would
// silently change answers.
func TestFallbackAgreesWithIndexedPath(t *testing.T) {
	setupPreMigrationZips(t, false)
	fallback, err := FindZipCodesWithinRadius("43215", 10, 25)
	if err != nil {
		t.Fatalf("fallback: %v", err)
	}

	setupPreMigrationZips(t, true)
	indexed, err := FindZipCodesWithinRadius("43215", 10, 25)
	if err != nil {
		t.Fatalf("indexed: %v", err)
	}

	if len(fallback) != len(indexed) {
		t.Fatalf("row count differs: fallback %d, indexed %d", len(fallback), len(indexed))
	}
	for i := range fallback {
		if fallback[i].ZipCode.ZipCode != indexed[i].ZipCode.ZipCode {
			t.Errorf("position %d: fallback %s, indexed %s",
				i, fallback[i].ZipCode.ZipCode, indexed[i].ZipCode.ZipCode)
		}
		if diff := fallback[i].DistanceMiles - indexed[i].DistanceMiles; diff > 1e-9 || diff < -1e-9 {
			t.Errorf("%s distance differs: fallback %.12f, indexed %.12f",
				fallback[i].ZipCode.ZipCode, fallback[i].DistanceMiles, indexed[i].DistanceMiles)
		}
	}
	t.Logf("fallback and indexed agree on %d rows", len(indexed))
}
