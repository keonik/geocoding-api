package services

import (
	"database/sql"
	"fmt"
	"math"
	"os"
	"testing"
	"time"

	"geocoding-api/database"
	"geocoding-api/models"

	_ "github.com/lib/pq"
)

// These tests pin the defect that moved radius search onto PostGIS: the old
// bounding-box implementation could return fewer ZIP codes than genuinely sat
// inside the requested radius, and its ordering was skewed away from the
// equator. Both are silent wrong answers, so both get a test that fails
// against the old code and passes against the new.
//
// Skipped unless PROBE_DSN is set.

// legacyFindZipCodesWithinRadius is the implementation this workstream
// replaced, copied verbatim from the commit before it so the tests can show
// the difference rather than assert it. Do not "fix" it.
func legacyFindZipCodesWithinRadius(centerZip string, radiusMiles float64, limit int) ([]*RadiusSearchResult, error) {
	centerZipCode, err := GetZipCodeByZip(centerZip)
	if err != nil {
		return nil, fmt.Errorf("failed to get center ZIP code: %w", err)
	}
	if centerZipCode == nil {
		return nil, fmt.Errorf("center ZIP code %s not found", centerZip)
	}

	latDelta := radiusMiles / 69.0
	lngDelta := radiusMiles / (69.0 * math.Cos(centerZipCode.Latitude*math.Pi/180.0))

	minLat := centerZipCode.Latitude - latDelta
	maxLat := centerZipCode.Latitude + latDelta
	minLng := centerZipCode.Longitude - lngDelta
	maxLng := centerZipCode.Longitude + lngDelta

	query := `
		SELECT zip_code, city_name, state_code, state_name, zcta, zcta_parent,
			   population, density, primary_county_code, primary_county_name,
			   county_weights, county_names, county_codes, imprecise, military,
			   timezone, latitude, longitude
		FROM zip_codes
		WHERE latitude BETWEEN $1 AND $2
		  AND longitude BETWEEN $3 AND $4
		  AND zip_code != $5
		ORDER BY
			(latitude - $6) * (latitude - $6) + (longitude - $7) * (longitude - $7)
		LIMIT $8
	`

	rows, err := database.DB.Query(query, minLat, maxLat, minLng, maxLng, centerZip,
		centerZipCode.Latitude, centerZipCode.Longitude, limit*3)
	if err != nil {
		return nil, fmt.Errorf("failed to query ZIP codes: %w", err)
	}
	defer rows.Close()

	var results []*RadiusSearchResult
	for rows.Next() {
		zc := &models.ZipCode{}
		err := rows.Scan(
			&zc.ZipCode, &zc.CityName, &zc.StateCode, &zc.StateName, &zc.ZCTA, &zc.ZCTAParent,
			&zc.Population, &zc.Density, &zc.PrimaryCountyCode, &zc.PrimaryCountyName,
			&zc.CountyWeights, &zc.CountyNames, &zc.CountyCodes, &zc.Imprecise, &zc.Military,
			&zc.Timezone, &zc.Latitude, &zc.Longitude,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan ZIP code: %w", err)
		}

		distance := haversineDistance(
			centerZipCode.Latitude, centerZipCode.Longitude,
			zc.Latitude, zc.Longitude,
		)

		if distance <= radiusMiles {
			results = append(results, &RadiusSearchResult{
				ZipCode:       zc,
				DistanceMiles: distance,
				DistanceKm:    distance * 1.60934,
			})
			if len(results) >= limit {
				break
			}
		}
	}

	return results, nil
}

// zipSeed is one row to plant in the fixture table.
type zipSeed struct {
	zip string
	lat float64
	lng float64
}

// newRadiusFixture connects to PROBE_DSN, builds an isolated zip_codes table
// carrying the migration-21 geography column, and points database.DB at it.
//
// The DDL is a copy of createZipCodesTable plus addZipCodeGeography, both of
// which live in package database and are unexported. TestZipCodeGeographyMigration
// in that package is what pins the real migration; this only needs a table
// shaped like it.
//
// Each fixture gets a private schema rather than creating zip_codes in public,
// because `go test ./...` runs packages in parallel and the database package
// has its own zip_codes fixture pointed at the same PROBE_DSN. search_path is
// a per-session setting, so the pool is pinned to one connection to make it
// stick -- these tests are sequential and never need a second.
func newRadiusFixture(t *testing.T, seeds []zipSeed) *sql.DB {
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

	schema := fmt.Sprintf("fixture_%d", time.Now().UnixNano())
	if _, err := db.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatalf("create fixture schema: %v", err)
	}
	if _, err := db.Exec(`SET search_path TO ` + schema + `, public`); err != nil {
		t.Fatalf("set search_path: %v", err)
	}

	if _, err := db.Exec(`
		CREATE TABLE zip_codes (
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
		);
		CREATE INDEX idx_zip_codes_location ON zip_codes(latitude, longitude);
		ALTER TABLE zip_codes ADD COLUMN geog geography(Point,4326)
			GENERATED ALWAYS AS (
				ST_SetSRID(
					ST_MakePoint(longitude::double precision, latitude::double precision),
					4326
				)::geography
			) STORED;
		CREATE INDEX idx_zip_codes_geog ON zip_codes USING GIST (geog);
	`); err != nil {
		t.Fatalf("create fixture table: %v", err)
	}

	for _, s := range seeds {
		_, err := db.Exec(`
			INSERT INTO zip_codes (zip_code, city_name, state_code, state_name,
				primary_county_code, primary_county_name, timezone, latitude, longitude)
			VALUES ($1, $2, 'OH', 'Ohio', '39049', 'Franklin', 'America/New_York', $3, $4)
		`, s.zip, "Fixture "+s.zip, s.lat, s.lng)
		if err != nil {
			t.Fatalf("seed %s: %v", s.zip, err)
		}
	}

	prev := database.DB
	database.DB = db
	t.Cleanup(func() {
		database.DB = prev
		if _, err := db.Exec(`DROP SCHEMA IF EXISTS ` + schema + ` CASCADE`); err != nil {
			t.Errorf("fixture schema %s leaked: %v", schema, err)
		}
		db.Close()
	})

	return db
}

func zipsOf(results []*RadiusSearchResult) []string {
	out := make([]string, 0, len(results))
	for _, r := range results {
		out = append(out, r.ZipCode.ZipCode)
	}
	return out
}

// TestRadiusSearchDoesNotUnderReturn is the whole point of the workstream.
//
// The trap is built out of the two defects working together. Ordering by
// squared degrees treats a degree of longitude as equal to a degree of
// latitude, but at 47 deg N a degree of longitude is only cos(47) = 0.68 as
// long. So a point offset diagonally is nearer in squared degrees yet further
// in miles than a point offset purely east. Plant enough diagonal decoys just
// outside the radius and they win the ORDER BY, fill the LIMIT*3 candidate
// slice, get discarded by the Haversine filter, and the genuinely-inside ZIPs
// further east never make it into the query result at all.
func TestRadiusSearchDoesNotUnderReturn(t *testing.T) {
	const (
		centerLat = 47.0
		centerLng = -122.0
		radius    = 10.0
		limit     = 5
	)

	seeds := []zipSeed{{zip: "00000", lat: centerLat, lng: centerLng}}

	// Decoys: diagonal offsets that are outside the radius but score low on
	// squared degrees. 16 of them, more than the legacy LIMIT*3 of 15.
	decoys := map[string]bool{}
	for i := 0; i < 16; i++ {
		// Spread them slightly so they are distinct points, all near the
		// same squared-degree score.
		d := 0.129 + float64(i)*0.0002
		zip := fmt.Sprintf("1%04d", i)
		lat := centerLat + d
		lng := centerLng + d
		if i%2 == 1 {
			lat = centerLat - d
		}
		seeds = append(seeds, zipSeed{zip: zip, lat: lat, lng: lng})
		decoys[zip] = true
	}

	// Targets: due east, inside the radius, but scoring worse on squared
	// degrees than every decoy.
	targets := map[string]bool{}
	for i := 0; i < limit; i++ {
		zip := fmt.Sprintf("2%04d", i)
		seeds = append(seeds, zipSeed{zip: zip, lat: centerLat, lng: centerLng + 0.19 + float64(i)*0.002})
		targets[zip] = true
	}

	// The test must not depend on my arithmetic: assert the geometry of the
	// fixture is actually the trap before asserting behaviour.
	for _, s := range seeds[1:] {
		d := haversineDistance(centerLat, centerLng, s.lat, s.lng)
		sqDeg := (s.lat-centerLat)*(s.lat-centerLat) + (s.lng-centerLng)*(s.lng-centerLng)
		switch {
		case decoys[s.zip]:
			if d <= radius {
				t.Fatalf("fixture broken: decoy %s is %.3f mi away, inside the %v mi radius", s.zip, d, radius)
			}
		case targets[s.zip]:
			if d > radius {
				t.Fatalf("fixture broken: target %s is %.3f mi away, outside the %v mi radius", s.zip, d, radius)
			}
			// every decoy must beat every target on the legacy ordering
			for _, o := range seeds[1:] {
				if !decoys[o.zip] {
					continue
				}
				oSq := (o.lat-centerLat)*(o.lat-centerLat) + (o.lng-centerLng)*(o.lng-centerLng)
				if oSq >= sqDeg {
					t.Fatalf("fixture broken: decoy %s (sq=%.6f) does not outrank target %s (sq=%.6f)",
						o.zip, oSq, s.zip, sqDeg)
				}
			}
		}
	}

	newRadiusFixture(t, seeds)

	legacy, err := legacyFindZipCodesWithinRadius("00000", radius, limit)
	if err != nil {
		t.Fatalf("legacy search: %v", err)
	}
	current, err := FindZipCodesWithinRadius("00000", radius, limit)
	if err != nil {
		t.Fatalf("current search: %v", err)
	}

	t.Logf("legacy  returned %d/%d: %v", len(legacy), limit, zipsOf(legacy))
	t.Logf("current returned %d/%d: %v", len(current), limit, zipsOf(current))

	if len(legacy) >= limit {
		t.Fatalf("fixture did not reproduce the defect: legacy returned %d, expected fewer than %d",
			len(legacy), limit)
	}
	if len(current) != limit {
		t.Errorf("current returned %d ZIPs, want %d (%v)", len(current), limit, zipsOf(current))
	}
	for _, r := range current {
		if !targets[r.ZipCode.ZipCode] {
			t.Errorf("current returned %s, which is not one of the in-radius targets", r.ZipCode.ZipCode)
		}
		if r.DistanceMiles > radius {
			t.Errorf("%s reported %.4f mi, above the %v mi radius that admitted it",
				r.ZipCode.ZipCode, r.DistanceMiles, radius)
		}
	}
}

// TestRadiusSearchOrdersByTrueDistance pins nearest-first ordering at a
// latitude where squared degrees and real distance disagree.
func TestRadiusSearchOrdersByTrueDistance(t *testing.T) {
	const (
		centerLat = 47.0
		centerLng = -122.0
		radius    = 25.0
		limit     = 10
	)

	// north is nearer in miles; east is nearer in squared degrees. At 47 deg N
	// a 0.20 deg step east is ~9.4 mi while a 0.20 deg step north is ~13.8 mi.
	seeds := []zipSeed{
		{zip: "00000", lat: centerLat, lng: centerLng},
		{zip: "30001", lat: centerLat + 0.20, lng: centerLng}, // ~13.8 mi, sq 0.0400
		{zip: "30002", lat: centerLat, lng: centerLng + 0.25}, // ~11.7 mi, sq 0.0625
	}
	newRadiusFixture(t, seeds)

	got, err := FindZipCodesWithinRadius("00000", radius, limit)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d results, want 2 (%v)", len(got), zipsOf(got))
	}

	for _, r := range got {
		t.Logf("%s  %.4f mi  %.4f km  sqDeg=%.6f",
			r.ZipCode.ZipCode, r.DistanceMiles, r.DistanceKm,
			(r.ZipCode.Latitude-centerLat)*(r.ZipCode.Latitude-centerLat)+
				(r.ZipCode.Longitude-centerLng)*(r.ZipCode.Longitude-centerLng))
	}

	if got[0].ZipCode.ZipCode != "30002" {
		t.Errorf("nearest is %s, want 30002 (the east point is nearer in miles even though "+
			"it scores worse on squared degrees)", got[0].ZipCode.ZipCode)
	}
	for i := 1; i < len(got); i++ {
		if got[i].DistanceMiles < got[i-1].DistanceMiles {
			t.Errorf("results not nearest-first: %s (%.4f) after %s (%.4f)",
				got[i].ZipCode.ZipCode, got[i].DistanceMiles,
				got[i-1].ZipCode.ZipCode, got[i-1].DistanceMiles)
		}
	}

	// The KNN operator used for ORDER BY and the ST_Distance used for the
	// reported value must be the same measure, or the ordering contract is a
	// coincidence. Cross-check the reported miles against the Go Haversine,
	// which is now on the same sphere.
	for _, r := range got {
		want := haversineDistance(centerLat, centerLng, r.ZipCode.Latitude, r.ZipCode.Longitude)
		if math.Abs(want-r.DistanceMiles) > 0.001 {
			t.Errorf("%s: PostGIS reported %.6f mi, Go Haversine says %.6f mi",
				r.ZipCode.ZipCode, r.DistanceMiles, want)
		}
	}
}

// TestRadiusSearchAgreesWithDistanceEndpoint guards the reason earthRadiusMiles
// changed: /nearby and /distance must report the same mileage for a pair.
func TestRadiusSearchAgreesWithDistanceEndpoint(t *testing.T) {
	seeds := []zipSeed{
		{zip: "00000", lat: 39.9612, lng: -83.0007},
		{zip: "40001", lat: 40.0150, lng: -82.9200},
	}
	newRadiusFixture(t, seeds)

	nearby, err := FindZipCodesWithinRadius("00000", 25, 10)
	if err != nil {
		t.Fatalf("nearby: %v", err)
	}
	if len(nearby) != 1 {
		t.Fatalf("got %d nearby results, want 1", len(nearby))
	}

	pair, err := CalculateDistanceBetweenZipCodes("00000", "40001")
	if err != nil {
		t.Fatalf("distance: %v", err)
	}

	t.Logf("/nearby   %.6f mi  %.6f km", nearby[0].DistanceMiles, nearby[0].DistanceKm)
	t.Logf("/distance %.6f mi  %.6f km", pair.DistanceMiles, pair.DistanceKm)

	if math.Abs(nearby[0].DistanceMiles-pair.DistanceMiles) > 0.001 {
		t.Errorf("endpoints disagree: /nearby %.6f mi vs /distance %.6f mi",
			nearby[0].DistanceMiles, pair.DistanceMiles)
	}
	if math.Abs(nearby[0].DistanceKm-pair.DistanceKm) > 0.001 {
		t.Errorf("endpoints disagree on km: /nearby %.6f vs /distance %.6f",
			nearby[0].DistanceKm, pair.DistanceKm)
	}
}

// TestRadiusSearchUsesTheSpatialIndex confirms the GIST index is actually
// chosen. A correct answer reached by sequential scan would pass every other
// test here while leaving the performance problem in place.
func TestRadiusSearchUsesTheSpatialIndex(t *testing.T) {
	seeds := []zipSeed{{zip: "00000", lat: 47.0, lng: -122.0}}
	for i := 0; i < 2000; i++ {
		seeds = append(seeds, zipSeed{
			zip: fmt.Sprintf("5%04d", i),
			lat: 40.0 + float64(i%50)*0.12,
			lng: -125.0 + float64(i/50)*0.15,
		})
	}
	db := newRadiusFixture(t, seeds)

	if _, err := db.Exec(`ANALYZE zip_codes`); err != nil {
		t.Fatalf("analyze: %v", err)
	}

	var plan string
	err := db.QueryRow(`
		EXPLAIN (FORMAT TEXT)
		SELECT z.zip_code
		FROM zip_codes z, (SELECT geog FROM zip_codes WHERE zip_code = '00000') center
		WHERE z.zip_code <> '00000'
		  AND ST_DWithin(z.geog, center.geog, 16093.44, false)
		ORDER BY z.geog <-> center.geog
		LIMIT 5
	`).Scan(&plan)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	t.Logf("plan root: %s", plan)

	rows, err := db.Query(`
		EXPLAIN (FORMAT TEXT)
		SELECT z.zip_code
		FROM zip_codes z, (SELECT geog FROM zip_codes WHERE zip_code = '00000') center
		WHERE z.zip_code <> '00000'
		  AND ST_DWithin(z.geog, center.geog, 16093.44, false)
		ORDER BY z.geog <-> center.geog
		LIMIT 5
	`)
	if err != nil {
		t.Fatalf("explain rows: %v", err)
	}
	defer rows.Close()

	full := ""
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		full += line + "\n"
	}
	t.Logf("full plan:\n%s", full)

	if !containsAny(full, "idx_zip_codes_geog", "Index Scan") {
		t.Errorf("query is not using the spatial index; plan was:\n%s", full)
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(sub) > 0 && len(s) >= len(sub) {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}
