package services

import (
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"geocoding-api/models"

	_ "github.com/lib/pq"
)

const filterMatrixSchema = "filter_matrix_probe"

// Every filter the search accepts contributes its own parameters, and they
// have to be numbered in the order their values are bound. A mismatch does not
// raise: the query runs with a state filter reading a bounding box's longitude
// and returns the wrong rows. Individual filters were covered; the
// combinations, where the numbering actually goes wrong, were not.
//
// So this checks combinations against an expectation computed in Go from the
// same fixture, rather than checking that the query merely ran.
type matrixRow struct {
	id       int64
	house    string
	street   string
	city     string
	county   string
	region   string
	postcode string
	lng, lat float64
}

var matrixRows = []matrixRow{
	{1, "100", "Main Street", "Columbus", "Franklin", "OH", "43215", -83.0007, 39.9612},
	{2, "200", "Main Street", "Columbus", "Franklin", "OH", "43215", -83.0020, 39.9615},
	{3, "300", "High Street", "Columbus", "Franklin", "OH", "43201", -83.0050, 39.9700},
	{4, "400", "Main Street", "Cleveland", "Cuyahoga", "OH", "44113", -81.6931, 41.5045},
	{5, "500", "Euclid Avenue", "Cleveland", "Cuyahoga", "OH", "44114", -81.6800, 41.5100},
	{6, "600", "Main Street", "Indianapolis", "Marion", "IN", "46204", -86.1581, 39.7684},
	{7, "700", "Meridian Street", "Indianapolis", "Marion", "IN", "46204", -86.1600, 39.7700},
	{8, "800", "Main Street", "Toledo", "Lucas", "OH", "43604", -83.5552, 41.6528},
}

func setupFilterMatrixDB(t *testing.T) *sql.DB {
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
		if _, err := db.Exec("DROP SCHEMA IF EXISTS " + filterMatrixSchema + " CASCADE"); err != nil {
			t.Logf("cleanup: %v", err)
		}
		db.Close()
	})

	stmts := []string{
		"CREATE EXTENSION IF NOT EXISTS postgis",
		"CREATE EXTENSION IF NOT EXISTS pg_trgm",
		"DROP SCHEMA IF EXISTS " + filterMatrixSchema + " CASCADE",
		"CREATE SCHEMA " + filterMatrixSchema,
		fmt.Sprintf("SET search_path TO %s, public", filterMatrixSchema),
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
	}
	for _, r := range matrixRows {
		full := fmt.Sprintf("%s %s, %s, %s %s", r.house, r.street, r.city, r.region, r.postcode)
		stmts = append(stmts, fmt.Sprintf(
			`INSERT INTO ohio_addresses (id, hash, house_number, street, unit, city, district, region, postcode, county, geom, full_address)
			 VALUES (%d, 'm%d', '%s', '%s', '', '%s', 'XXX', '%s', '%s', '%s',
			         ST_SetSRID(ST_MakePoint(%g, %g), 4326), '%s')`,
			r.id, r.id, r.house, r.street, r.city, r.region, r.postcode, r.county, r.lng, r.lat, full))
	}
	stmts = append(stmts, "ANALYZE ohio_addresses", "SELECT setval(pg_get_serial_sequence('ohio_addresses','id'), 100)")
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("setup failed on %.70q: %v", stmt, err)
		}
	}
	createBoundaryTables(t, db)
	return db
}

// ids of the rows a set of structured filters should return, computed here
// rather than in SQL so the two can disagree.
func expectedIDs(match func(matrixRow) bool) []int64 {
	var ids []int64
	for _, r := range matrixRows {
		if match(r) {
			ids = append(ids, r.id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func gotIDs(t *testing.T, svc *AddressService, params models.AddressSearchParams) []int64 {
	t.Helper()
	if params.Limit == 0 {
		params.Limit = 50
	}
	found, _, err := svc.SearchAddresses(params)
	if err != nil {
		t.Fatalf("search %+v: %v", params, err)
	}
	ids := make([]int64, 0, len(found))
	for _, a := range found {
		ids = append(ids, a.ID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func TestSearchFilterCombinations(t *testing.T) {
	db := setupFilterMatrixDB(t)
	svc := NewAddressService(db)

	// A box around Columbus, and one around Cleveland.
	columbus := &models.BoundingBox{MinLng: -83.01, MinLat: 39.95, MaxLng: -82.99, MaxLat: 39.98}
	cleveland := &models.BoundingBox{MinLng: -81.70, MinLat: 41.50, MaxLng: -81.67, MaxLat: 41.52}
	inBox := func(b *models.BoundingBox) func(matrixRow) bool {
		return func(r matrixRow) bool {
			return r.lng >= b.MinLng && r.lng <= b.MaxLng && r.lat >= b.MinLat && r.lat <= b.MaxLat
		}
	}

	cases := []struct {
		name   string
		params models.AddressSearchParams
		match  func(matrixRow) bool
	}{
		{"state only", models.AddressSearchParams{State: "OH"},
			func(r matrixRow) bool { return r.region == "OH" }},
		{"state lowercased", models.AddressSearchParams{State: "oh"},
			func(r matrixRow) bool { return r.region == "OH" }},
		{"county only", models.AddressSearchParams{County: "Franklin"},
			func(r matrixRow) bool { return r.county == "Franklin" }},
		{"city only", models.AddressSearchParams{City: "Cleveland"},
			func(r matrixRow) bool { return r.city == "Cleveland" }},
		{"postcode only", models.AddressSearchParams{Postcode: "43215"},
			func(r matrixRow) bool { return r.postcode == "43215" }},
		{"street only", models.AddressSearchParams{Street: "Main"},
			func(r matrixRow) bool { return strings.Contains(r.street, "Main") }},
		{"bbox only", models.AddressSearchParams{BBox: columbus}, inBox(columbus)},

		// Pairs: the state filter's parameter now sits before or after
		// another filter's, which is where hand-numbering goes wrong.
		{"state and county", models.AddressSearchParams{State: "OH", County: "Cuyahoga"},
			func(r matrixRow) bool { return r.region == "OH" && r.county == "Cuyahoga" }},
		{"state and city", models.AddressSearchParams{State: "IN", City: "Indianapolis"},
			func(r matrixRow) bool { return r.region == "IN" && r.city == "Indianapolis" }},
		{"state and bbox", models.AddressSearchParams{State: "OH", BBox: cleveland},
			func(r matrixRow) bool { return r.region == "OH" && inBox(cleveland)(r) }},
		{"bbox and street", models.AddressSearchParams{BBox: columbus, Street: "Main"},
			func(r matrixRow) bool { return inBox(columbus)(r) && strings.Contains(r.street, "Main") }},
		{"county and postcode", models.AddressSearchParams{County: "Franklin", Postcode: "43201"},
			func(r matrixRow) bool { return r.county == "Franklin" && r.postcode == "43201" }},

		// Everything at once, in the order the builder binds them.
		{"state, bbox, county, city, postcode, street",
			models.AddressSearchParams{State: "OH", BBox: columbus, County: "Franklin",
				City: "Columbus", Postcode: "43215", Street: "Main"},
			func(r matrixRow) bool {
				return r.region == "OH" && inBox(columbus)(r) && r.county == "Franklin" &&
					r.city == "Columbus" && r.postcode == "43215" && strings.Contains(r.street, "Main")
			}},

		// A combination that should match nothing: the filters are real but
		// contradictory, which a misnumbered parameter would turn into hits.
		{"contradictory state and city", models.AddressSearchParams{State: "IN", City: "Cleveland"},
			func(r matrixRow) bool { return false }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := expectedIDs(tc.match)
			got := gotIDs(t, svc, tc.params)
			if fmt.Sprint(got) != fmt.Sprint(want) {
				t.Errorf("ids = %v, want %v", got, want)
			}
		})
	}
}

// Proximity binds three parameters, and distance ordering two more after the
// WHERE clause is closed. Both are bound last, which is exactly where an
// off-by-one lands.
func TestSearchProximityAndOrdering(t *testing.T) {
	db := setupFilterMatrixDB(t)
	svc := NewAddressService(db)

	// 5km around downtown Columbus reaches the three Columbus rows only.
	near := models.AddressSearchParams{Lat: 39.9612, Lng: -83.0007, Radius: 5, Limit: 50}
	if got, want := gotIDs(t, svc, near), []int64{1, 2, 3}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("proximity ids = %v, want %v", got, want)
	}

	// With a state filter in front of it, so its parameters are numbered after
	// the state's.
	withState := near
	withState.State = "OH"
	if got, want := gotIDs(t, svc, withState), []int64{1, 2, 3}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("proximity with state ids = %v, want %v", got, want)
	}

	// Ordering: nearest first, and the total is the filtered count rather than
	// the table's.
	found, total, err := svc.SearchAddresses(models.AddressSearchParams{
		Lat: 39.9612, Lng: -83.0007, Radius: 5, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) == 0 || found[0].ID != 1 {
		t.Errorf("nearest is %v, want id 1", found)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3: the count query must take the WHERE parameters and no others", total)
	}

	// Pagination binds LIMIT and OFFSET after everything else.
	page2 := models.AddressSearchParams{State: "OH", Limit: 2, Offset: 2}
	if got := gotIDs(t, svc, page2); len(got) != 2 {
		t.Errorf("page 2 returned %v, want two rows", got)
	}
}

// A text query binds one parameter per word for the fuzzy pass, or one
// tsquery, and the SELECT clause then references those same parameters.
func TestSearchTextWithFilters(t *testing.T) {
	db := setupFilterMatrixDB(t)
	svc := NewAddressService(db)

	// Text and a state filter: every hit is in that state and mentions the
	// word. The exact set is the query planner's business, not this test's.
	found, _, err := svc.SearchAddresses(models.AddressSearchParams{
		Query: "main street", State: "IN", Limit: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) == 0 {
		t.Fatal("no hits for a word that is in the data")
	}
	for _, a := range found {
		if a.Region != "IN" {
			t.Errorf("%s is in %s, not the requested IN", a.FullAddress, a.Region)
		}
		if !strings.Contains(strings.ToLower(a.FullAddress), "main") {
			t.Errorf("%s does not contain the query word", a.FullAddress)
		}
		if a.Match == nil || a.Match.Confidence == nil {
			t.Errorf("%s carries no match confidence, so the score is not being read from the predicate's own parameter", a.FullAddress)
		}
	}

	// The fuzzy fallback: a misspelling, with a filter in front of it.
	fuzzy, _, err := svc.SearchAddresses(models.AddressSearchParams{
		Query: "maim streat", State: "OH", Limit: 50,
	})
	if err != nil {
		t.Fatalf("fuzzy search: %v", err)
	}
	for _, a := range fuzzy {
		if a.Region != "OH" {
			t.Errorf("fuzzy hit %s is in %s, not OH", a.FullAddress, a.Region)
		}
	}
}
