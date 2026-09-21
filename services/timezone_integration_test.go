package services

import (
	"database/sql"
	"fmt"
	"os"
	"testing"

	"geocoding-api/database"
	"geocoding-api/models"

	_ "github.com/lib/pq"
)

const tzSchema = "tz_probe"

// The fixture straddles the Indiana-Illinois line, where the zone changes and
// the nearest ZIP is often on the wrong side of it.
//
//	47802 Terre Haute, IN   America/Indiana/Indianapolis
//	46011 Anderson, IN      America/Indiana/Indianapolis
//	61944 Paris, IL         America/Chicago
//	47620 Mount Vernon, IN  America/Chicago -- southwest Indiana is Central
//	47591 Vincennes, IN     America/Indiana/Vincennes
//
// borderPoint is in Indiana, 15km from Paris and 20km from Terre Haute: the
// nearest ZIP is the Illinois one, an hour off.
const (
	borderLat = 39.58
	borderLng = -87.52
)

// withGeog adds the generated geog column zip_codes gets in migration 21.
// Without it the fixture has the pre-migration shape.
func setupTimezoneDB(t *testing.T, withGeog bool) *sql.DB {
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

	geog := ""
	if withGeog {
		geog = `, geog geography(Point,4326) GENERATED ALWAYS AS
			(ST_SetSRID(ST_MakePoint(longitude::double precision, latitude::double precision), 4326)::geography) STORED`
	}

	stmts := []string{
		"CREATE EXTENSION IF NOT EXISTS postgis",
		"CREATE EXTENSION IF NOT EXISTS pg_trgm",
		fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", tzSchema),
		fmt.Sprintf("CREATE SCHEMA %s", tzSchema),
		// public is on the path for PostGIS. Every table the code under test
		// reads is created here, so none resolves to a real one in public.
		fmt.Sprintf("SET search_path TO %s, public", tzSchema),
		`CREATE TABLE zip_codes (
			zip_code VARCHAR(10) PRIMARY KEY,
			city_name VARCHAR(255) NOT NULL,
			state_code VARCHAR(2) NOT NULL,
			state_name VARCHAR(255) NOT NULL,
			zcta BOOLEAN NOT NULL DEFAULT FALSE,
			zcta_parent VARCHAR(10),
			population DECIMAL(12,2),
			density DECIMAL(10,2),
			primary_county_code VARCHAR(10) NOT NULL DEFAULT '',
			primary_county_name VARCHAR(255) NOT NULL DEFAULT '',
			county_weights JSONB DEFAULT '{}'::jsonb,
			county_names TEXT DEFAULT '',
			county_codes TEXT DEFAULT '',
			imprecise BOOLEAN NOT NULL DEFAULT FALSE,
			military BOOLEAN NOT NULL DEFAULT FALSE,
			timezone VARCHAR(100) NOT NULL,
			latitude DECIMAL(10,7) NOT NULL,
			longitude DECIMAL(10,7) NOT NULL` + geog + `)`,
		`INSERT INTO zip_codes (zip_code, city_name, state_code, state_name, timezone, latitude, longitude) VALUES
		 ('47802','Terre Haute','IN','Indiana','America/Indiana/Indianapolis',39.43,-87.40),
		 ('46011','Anderson','IN','Indiana','America/Indiana/Indianapolis',40.10,-85.68),
		 ('61944','Paris','IL','Illinois','America/Chicago',39.61,-87.69),
		 ('47620','Mount Vernon','IN','Indiana','America/Chicago',37.93,-87.89),
		 ('47591','Vincennes','IN','Indiana','America/Indiana/Vincennes',38.68,-87.53)`,
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
		// hash doubles as a handle for the assertions.
		fmt.Sprintf(`INSERT INTO ohio_addresses (hash, house_number, street, unit, city, district, region, postcode, county, geom, full_address) VALUES
		 ('zip4',     '1','Wabash Avenue','','Terre Haute','VIG','IN','47802-1234','Vigo',
		  ST_SetSRID(ST_MakePoint(-87.41,39.46),4326),'1 Wabash Avenue, Terre Haute, IN 47802'),
		 ('border',   '2','State Line Road','','Terre Haute','VIG','IN','','Vigo',
		  ST_SetSRID(ST_MakePoint(%g,%g),4326),'2 State Line Road, Terre Haute, IN'),
		 ('unknownzip','3','Meridian Street','','Anderson','MAD','IN','46999','Madison',
		  ST_SetSRID(ST_MakePoint(-85.70,40.12),4326),'3 Meridian Street, Anderson, IN 46999'),
		 ('ownzip',   '5','Ford Road','','Mount Vernon','POS','IN','47620-0001','Posey',
		  ST_SetSRID(ST_MakePoint(-87.70,38.40),4326),'5 Ford Road, Mount Vernon, IN 47620'),
		 ('remote',   '4','Hollow Road','','Nowhere','XXX','IN','46999','Nowhere',
		  ST_SetSRID(ST_MakePoint(-86.50,38.20),4326),'4 Hollow Road, Nowhere, IN 46999')`, borderLng, borderLat),
		`CREATE TABLE ohio_counties (
			id BIGSERIAL PRIMARY KEY, county_name VARCHAR(255) NOT NULL,
			bounds_geometry GEOMETRY(MULTIPOLYGON, 4326))`,
		`CREATE TABLE us_states (
			id BIGSERIAL PRIMARY KEY, state_fips VARCHAR(2) NOT NULL UNIQUE,
			state_abbr VARCHAR(2) NOT NULL UNIQUE, state_name VARCHAR(255) NOT NULL UNIQUE,
			geometry GEOMETRY(MULTIPOLYGON, 4326))`,
		`INSERT INTO us_states (state_fips, state_abbr, state_name, geometry) VALUES
		 ('18','IN','Indiana',  ST_Multi(ST_MakeEnvelope(-87.53,37.8,-84.8,41.8,4326))),
		 ('17','IL','Illinois', ST_Multi(ST_MakeEnvelope(-91.5,37.0,-87.53,42.5,4326)))`,
		"ANALYZE zip_codes",
		"ANALYZE ohio_addresses",
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("setup failed on %.60q: %v", stmt, err)
		}
	}

	// The timezone lookup reads the boundary tables; without them here it
	// would resolve them through public and answer from real data.
	createBoundaryTables(t, db)

	prev := database.DB
	database.DB = db
	t.Cleanup(func() {
		database.DB = prev
		if _, err := db.Exec(fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", tzSchema)); err != nil {
			t.Logf("cleanup: %v", err)
		}
		db.Close()
	})
	return db
}

func addressByHash(t *testing.T, db *sql.DB, hash string) *models.OhioAddress {
	t.Helper()
	var id int64
	if err := db.QueryRow("SELECT id FROM ohio_addresses WHERE hash = $1", hash).Scan(&id); err != nil {
		t.Fatalf("fixture %s: %v", hash, err)
	}
	a, err := NewAddressService(db).GetAddressByID(id)
	if err != nil {
		t.Fatalf("get %s: %v", hash, err)
	}
	return a
}

func zoneOf(tz *string) string {
	if tz == nil {
		return "<null>"
	}
	return *tz
}

func TestAddressTimezoneRules(t *testing.T) {
	db := setupTimezoneDB(t, true)

	cases := []struct {
		hash, want, why string
	}{
		{"zip4", "America/Indiana/Indianapolis", "a ZIP+4 postcode is matched on its first five digits"},
		// 34km from Vincennes and 55km from its own ZIP's centroid.
		{"ownzip", "America/Chicago", "the address's own ZIP wins over a nearer one in another zone"},
		{"border", "America/Indiana/Indianapolis", "with no postcode, the nearest ZIP in the same state wins over a closer one across the line"},
		{"unknownzip", "America/Indiana/Indianapolis", "a postcode missing from zip_codes falls back to the nearest ZIP"},
		// Over 100km from Vincennes, its nearest ZIP. The fallback runs per
		// row and stays bounded; see timezoneSearchMeters.
		{"remote", "<null>", "the per-address fallback is bounded at 50km"},
	}
	for _, c := range cases {
		t.Run(c.hash, func(t *testing.T) {
			a := addressByHash(t, db, c.hash)
			if got := zoneOf(a.Timezone); got != c.want {
				t.Errorf("timezone = %s, want %s: %s", got, c.want, c.why)
			}
		})
	}
}

// Every path that returns an address has to describe it; a wrapper that one
// early return skipped would leave some results unlabelled.
func TestEveryAddressPathStatesAccuracyAndTimezone(t *testing.T) {
	db := setupTimezoneDB(t, true)
	svc := NewAddressService(db)

	check := func(t *testing.T, path string, addrs []models.OhioAddress) {
		t.Helper()
		if len(addrs) == 0 {
			t.Fatalf("%s returned nothing", path)
		}
		for _, a := range addrs {
			if a.Accuracy != models.AccuracyPoint {
				t.Errorf("%s: %s has accuracy %q", path, a.FullAddress, a.Accuracy)
			}
			if a.Hash != "remote" && a.Timezone == nil {
				t.Errorf("%s: %s has no timezone", path, a.FullAddress)
			}
		}
	}

	found, _, err := svc.SearchAddresses(models.AddressSearchParams{Query: "wabash", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	check(t, "search (prefix)", found)

	fuzzy, _, err := svc.SearchAddresses(models.AddressSearchParams{Query: "wabassh", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(fuzzy) > 0 && fuzzy[0].Match != nil && fuzzy[0].Match.Tier != models.MatchTierFuzzy {
		t.Fatalf("expected the fuzzy pass, got tier %s", fuzzy[0].Match.Tier)
	}
	check(t, "search (fuzzy)", fuzzy)

	filtered, _, err := svc.SearchAddresses(models.AddressSearchParams{State: "IN", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	check(t, "search (filter)", filtered)

	full, err := svc.FullTextSearchAddresses("1 Wabash Avenue, Terre Haute, IN 47802", 10)
	if err != nil {
		t.Fatal(err)
	}
	check(t, "full-text ("+full.SearchMethod+")", full.Addresses)

	v, err := ValidateAddress(db, "1 Wabash Ave, Terre Haute, IN 47802")
	if err != nil {
		t.Fatal(err)
	}
	if v.Address == nil {
		t.Fatal("validate found nothing")
	}
	check(t, "validate", []models.OhioAddress{*v.Address})

	b, err := BatchGeocode(db, []BatchItem{{Query: "wabash"}, {Query: "state line"}, {ZipCode: "47802"}})
	if err != nil {
		t.Fatal(err)
	}
	var batched []models.OhioAddress
	for _, r := range b.Results[:2] {
		if r.Address == nil {
			t.Fatalf("batch item missed: %+v", r)
		}
		batched = append(batched, *r.Address)
	}
	check(t, "batch", batched)
	// Batch describes all its hits at once; each has to get its own zone, not
	// its neighbour's.
	if batched[1].Hash != "border" || zoneOf(batched[1].Timezone) != "America/Indiana/Indianapolis" {
		t.Errorf("batch border hit: %s in %s", batched[1].Hash, zoneOf(batched[1].Timezone))
	}
	if z := b.Results[2].ZipCode; z == nil || z.Accuracy != models.AccuracyPostalCentroid {
		t.Errorf("batch ZIP result: %+v", z)
	}
}

func TestZipResultsArePostalCentroids(t *testing.T) {
	setupTimezoneDB(t, true)

	one, err := GetZipCodeByZip("47802")
	if err != nil || one == nil {
		t.Fatalf("get: %v %v", one, err)
	}
	byCity, err := SearchZipCodesByCity("Terre", "IN", 10)
	if err != nil || len(byCity) == 0 {
		t.Fatalf("by city: %v %v", byCity, err)
	}
	near, err := FindZipCodesWithinRadius("47802", 50, 10)
	if err != nil || len(near) == 0 {
		t.Fatalf("radius: %v %v", near, err)
	}

	all := append([]*models.ZipCode{one}, byCity...)
	for _, r := range near {
		all = append(all, r.ZipCode)
	}
	for _, z := range all {
		if z.Accuracy != models.AccuracyPostalCentroid {
			t.Errorf("ZIP %s has accuracy %q", z.ZipCode, z.Accuracy)
		}
	}
}

// The nearest ZIP and the zone at the point are different questions, and at
// a state line they have different answers.
func TestReverseTimezoneIsTheZoneAtThePoint(t *testing.T) {
	db := setupTimezoneDB(t, true)

	got, err := ReverseGeocode(db, borderLat, borderLng, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.Zip == nil || got.Zip.ZipCode != "61944" || got.Zip.Timezone != "America/Chicago" {
		t.Fatalf("fixture premise: nearest ZIP should be Paris, IL in Central time, got %+v", got.Zip)
	}
	if got.Zip.Accuracy != models.AccuracyPostalCentroid {
		t.Errorf("zip accuracy %q", got.Zip.Accuracy)
	}
	if zoneOf(got.Timezone) != "America/Indiana/Indianapolis" {
		t.Errorf("timezone at an Indiana point = %s, want Indiana's, not the nearer Illinois ZIP's", zoneOf(got.Timezone))
	}
	if got.Address == nil || got.Address.Accuracy != models.AccuracyPoint ||
		zoneOf(got.Address.Timezone) != "America/Indiana/Indianapolis" {
		t.Errorf("reverse address not described: %+v", got.Address)
	}

	// Inside a known state there is no bound: over 100km from any ZIP, the
	// nearest one in the same state is still the answer.
	remote, err := ReverseGeocode(db, 38.20, -86.50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if zoneOf(remote.Timezone) != "America/Indiana/Vincennes" {
		t.Errorf("sparse-area point in Indiana got %s, want the nearest Indiana ZIP's zone", zoneOf(remote.Timezone))
	}

	// In no state but 44km from Mount Vernon: within the bound, so the
	// nearest ZIP in any state answers.
	offshore, err := ReverseGeocode(db, 37.75, -87.45, 0)
	if err != nil {
		t.Fatal(err)
	}
	if offshore.State != nil {
		t.Fatalf("fixture premise: point should be in no state, got %s", offshore.State.Code)
	}
	if zoneOf(offshore.Timezone) != "America/Chicago" {
		t.Errorf("stateless point 44km from a ZIP got %s, want its zone", zoneOf(offshore.Timezone))
	}

	// Open water in Lake Michigan: no state, so the 50km bound applies, and
	// nothing is within it.
	lake, err := ReverseGeocode(db, 42.3, -87.0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if lake.Timezone != nil {
		t.Errorf("timezone %s for a point 50km+ from any ZIP", *lake.Timezone)
	}
}

// Before migration 21 zip_codes has no geog. Searches must still succeed, and
// only what needs geog is lost: the nearest-ZIP fallback, not the address's
// own ZIP.
func TestTimezoneDegradesBeforeGeogExists(t *testing.T) {
	db := setupTimezoneDB(t, false)

	own := addressByHash(t, db, "zip4")
	if own.Accuracy != models.AccuracyPoint {
		t.Errorf("accuracy %q without geog", own.Accuracy)
	}
	if zoneOf(own.Timezone) != "America/Indiana/Indianapolis" {
		t.Errorf("own-ZIP timezone = %s without geog; the postcode lookup does not need it", zoneOf(own.Timezone))
	}
	if border := addressByHash(t, db, "border"); border.Timezone != nil {
		t.Errorf("no-postcode address got %s without geog, which the fallback needs", *border.Timezone)
	}

	rev, err := ReverseGeocode(db, borderLat, borderLng, 0)
	if err != nil {
		t.Fatalf("reverse failed without zip_codes.geog: %v", err)
	}
	if rev.Timezone != nil {
		t.Errorf("reverse timezone %s without geog", *rev.Timezone)
	}
}

// An address gets the same exact zone as a point at the same place. Without
// this, one /reverse response could carry an exact timezone and a ZIP-level
// address.timezone that disagreed with it.
func TestAddressesUseTheZonePolygons(t *testing.T) {
	db := setupTimezoneDB(t, true)

	// A zone covering the Illinois side of the fixture's line, loaded and
	// available. The border address sits in it; its ZIP-derived answer is
	// Indiana's, so the two cannot be confused.
	for _, stmt := range []string{
		`INSERT INTO boundaries (layer, geoid, state_fips, name, geom) VALUES
		 ('timezone', 'America/Chicago', '00', 'America/Chicago',
		  ST_Multi(ST_MakeEnvelope(-89, 37, -87.5, 42, 4326)))`,
		`INSERT INTO boundary_loads (layer, state_fips, status, available, features, source_url)
		 VALUES ('timezone', '00', 'loaded', true, 1, 'fixture')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}

	border := addressByHash(t, db, "border")
	if zoneOf(border.Timezone) != "America/Chicago" {
		t.Errorf("address timezone = %s, want the containing zone polygon", zoneOf(border.Timezone))
	}
	if border.TimezoneSource != TimezoneBoundarySource {
		t.Errorf("timezone_source = %q, want %q", border.TimezoneSource, TimezoneBoundarySource)
	}

	// The same coordinate through /reverse: both halves of the response now
	// agree, and both say where the answer came from.
	rev, err := ReverseGeocode(db, borderLat, borderLng, 0)
	if err != nil {
		t.Fatal(err)
	}
	if zoneOf(rev.Timezone) != "America/Chicago" || rev.TimezoneSource != TimezoneBoundarySource {
		t.Errorf("reverse point: %s from %q", zoneOf(rev.Timezone), rev.TimezoneSource)
	}
	if rev.Address == nil || zoneOf(rev.Address.Timezone) != zoneOf(rev.Timezone) {
		t.Errorf("reverse address timezone %v disagrees with the point's %v",
			rev.Address.Timezone, rev.Timezone)
	}

	// An address outside every loaded zone keeps the ZIP answer, and says so.
	away := addressByHash(t, db, "zip4")
	if zoneOf(away.Timezone) != "America/Indiana/Indianapolis" || away.TimezoneSource != TimezoneZIPSource {
		t.Errorf("address outside the zones: %s from %q", zoneOf(away.Timezone), away.TimezoneSource)
	}
}
