package services

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"

	_ "github.com/lib/pq"
)

const reverseSchema = "reverse_probe"

func setupReverseDB(t *testing.T) *sql.DB {
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
		fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", reverseSchema),
		fmt.Sprintf("CREATE SCHEMA %s", reverseSchema),
		fmt.Sprintf("SET search_path TO %s, public", reverseSchema),
		`CREATE TABLE ohio_addresses (
			id BIGSERIAL PRIMARY KEY, hash VARCHAR(255) NOT NULL,
			house_number VARCHAR(50), street VARCHAR(255), unit VARCHAR(50),
			city VARCHAR(255), district VARCHAR(10), region VARCHAR(2) NOT NULL,
			postcode VARCHAR(10), county VARCHAR(255),
			geom GEOMETRY(POINT, 4326) NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP, full_address TEXT)`,
		"CREATE INDEX ON ohio_addresses USING GIST (geom)",
		`CREATE TABLE ohio_counties (
			id BIGSERIAL PRIMARY KEY, county_name VARCHAR(255) NOT NULL,
			bounds_geometry GEOMETRY(MULTIPOLYGON, 4326))`,
		`CREATE TABLE us_states (
			id BIGSERIAL PRIMARY KEY, state_fips VARCHAR(2) NOT NULL UNIQUE,
			state_abbr VARCHAR(2) NOT NULL UNIQUE, state_name VARCHAR(255) NOT NULL UNIQUE,
			geometry GEOMETRY(MULTIPOLYGON, 4326))`,
		`CREATE TABLE zip_codes (
			zip_code VARCHAR(10) PRIMARY KEY, city_name VARCHAR(255), state_code VARCHAR(2),
			latitude DOUBLE PRECISION, longitude DOUBLE PRECISION,
			geog geography(Point,4326))`,

		// Downtown Columbus and two neighbours a short walk away.
		`INSERT INTO ohio_addresses (hash, house_number, street, unit, city, district, region, postcode, county, geom, full_address) VALUES
		 ('r1','100','Main Street','','Columbus','FRA','OH','43215','Franklin',
		  ST_SetSRID(ST_MakePoint(-83.0007,39.9612),4326),'100 Main Street, Columbus, OH 43215'),
		 ('r2','200','Main Street','','Columbus','FRA','OH','43215','Franklin',
		  ST_SetSRID(ST_MakePoint(-83.0020,39.9615),4326),'200 Main Street, Columbus, OH 43215'),
		 ('r3','300','Far Road','','Toledo','LUC','OH','43617','Lucas',
		  ST_SetSRID(ST_MakePoint(-83.6000,41.6600),4326),'300 Far Road, Toledo, OH 43617')`,
		`INSERT INTO ohio_counties (county_name, bounds_geometry) VALUES
		 ('Franklin', ST_Multi(ST_MakeEnvelope(-83.2,39.8,-82.8,40.2,4326)))`,
		`INSERT INTO us_states (state_fips, state_abbr, state_name, geometry) VALUES
		 ('39','OH','Ohio', ST_Multi(ST_MakeEnvelope(-85.0,38.4,-80.5,42.0,4326)))`,
		`INSERT INTO zip_codes (zip_code, city_name, state_code, latitude, longitude, geog) VALUES
		 ('43215','Columbus','OH',39.9612,-83.0007, ST_SetSRID(ST_MakePoint(-83.0007,39.9612),4326)::geography),
		 ('43617','Toledo','OH',41.6600,-83.6000,   ST_SetSRID(ST_MakePoint(-83.6000,41.6600),4326)::geography)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("setup failed on %.60q: %v", stmt, err)
		}
	}

	t.Cleanup(func() {
		if _, err := db.Exec(fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", reverseSchema)); err != nil {
			t.Logf("cleanup: %v", err)
		}
		db.Close()
	})
	return db
}

// The whole point: a coordinate resolves to the things that describe it.
func TestReverseGeocodeResolvesAPoint(t *testing.T) {
	db := setupReverseDB(t)

	got, err := ReverseGeocode(db, 39.9613, -83.0008, 0)
	if err != nil {
		t.Fatalf("reverse: %v", err)
	}

	if got.Address == nil {
		t.Fatal("no address found for a point metres from one")
	}
	if got.Address.HouseNumber != "100" {
		t.Errorf("nearest address is %s, want the closer 100 Main Street", got.Address.FullAddress)
	}
	// Roughly 14m away; the assertion is that it is small and real, not zero.
	if got.Address.DistanceMeters <= 0 || got.Address.DistanceMeters > 100 {
		t.Errorf("distance %.1fm is not plausible for a point beside the address", got.Address.DistanceMeters)
	}
	if got.County == nil || *got.County != "Franklin" {
		t.Errorf("county = %v, want Franklin", got.County)
	}
	if got.State == nil || got.State.Code != "OH" {
		t.Errorf("state = %v, want OH", got.State)
	}
	if got.Zip == nil || got.Zip.ZipCode != "43215" {
		t.Errorf("zip = %v, want 43215", got.Zip)
	}
	t.Logf("resolved to %s (%.1fm), %s County, %s, ZIP %s",
		got.Address.FullAddress, got.Address.DistanceMeters, *got.County, got.State.Code, got.Zip.ZipCode)
}

// Without a bound, a point in the middle of a lake returns the nearest address
// on shore and presents it as the address there. A miss has to read as a miss.
func TestReverseGeocodeReturnsNoAddressBeyondTheRadius(t *testing.T) {
	db := setupReverseDB(t)

	// Roughly 8km from the Toledo row, well outside the default 2km.
	got, err := ReverseGeocode(db, 41.7300, -83.6000, 0)
	if err != nil {
		t.Fatalf("reverse: %v", err)
	}
	if got.Address != nil {
		t.Errorf("returned %s at %.0fm for a point outside the search radius",
			got.Address.FullAddress, got.Address.DistanceMeters)
	}

	// The other fields still answer -- a point can be in a state with no
	// address near it, and saying so beats forcing a single answer.
	if got.State == nil || got.State.Code != "OH" {
		t.Error("state should still resolve when no address is in range")
	}
	if got.Zip == nil {
		t.Error("nearest ZIP should still resolve when no address is in range")
	}

	// Widening the search finds it.
	wider, err := ReverseGeocode(db, 41.7300, -83.6000, 20000)
	if err != nil {
		t.Fatalf("reverse wide: %v", err)
	}
	if wider.Address == nil {
		t.Error("a 20km radius should reach the Toledo address")
	}
}

// A point outside every boundary should not invent containment.
func TestReverseGeocodeOutsideCoverageIsHonest(t *testing.T) {
	db := setupReverseDB(t)

	got, err := ReverseGeocode(db, 35.0, -40.0, 0)
	if err != nil {
		t.Fatalf("reverse: %v", err)
	}
	if got.Address != nil {
		t.Error("an address was returned for a point in the Atlantic")
	}
	if got.County != nil {
		t.Errorf("county %q was returned for a point in the Atlantic", *got.County)
	}
	if got.State != nil {
		t.Errorf("state %v was returned for a point in the Atlantic", got.State)
	}
	// The ZIP is explicitly nearest-centroid, not containment, so it answers --
	// and the distance says how little it means.
	if got.Zip != nil && got.Zip.DistanceMeters < 1000000 {
		t.Errorf("nearest ZIP reported at %.0fm from the Atlantic", got.Zip.DistanceMeters)
	}
}

func TestReverseGeocodeRejectsImpossibleCoordinates(t *testing.T) {
	db := setupReverseDB(t)

	for _, c := range []struct{ lat, lng float64 }{
		{91, -83}, {-91, -83}, {40, 181}, {40, -181},
	} {
		if _, err := ReverseGeocode(db, c.lat, c.lng, 0); err == nil {
			t.Errorf("(%g, %g) was accepted", c.lat, c.lng)
		}
	}
}

// The search has to ride the GIST index; a KNN scan of every address is not a
// reverse geocoder.
func TestReverseGeocodeUsesTheSpatialIndex(t *testing.T) {
	db := setupReverseDB(t)

	if _, err := db.Exec("SET enable_seqscan = off"); err != nil {
		t.Fatalf("disable seqscan: %v", err)
	}
	defer db.Exec("SET enable_seqscan = on")

	rows, err := db.Query(`
		EXPLAIN SELECT id FROM ohio_addresses
		WHERE ST_DWithin(geom::geography, ST_SetSRID(ST_MakePoint(-83.0,39.96),4326)::geography, 2000, false)
		ORDER BY geom <-> ST_SetSRID(ST_MakePoint(-83.0,39.96),4326) LIMIT 1`)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer rows.Close()

	var plan string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan: %v", err)
		}
		plan += line + "\n"
	}
	if !strings.Contains(plan, "Index Scan") && !strings.Contains(plan, "Bitmap Index Scan") {
		t.Errorf("nearest-address search cannot use the GIST index:\n%s", plan)
	}
	t.Logf("plan: %s", plan)
}
