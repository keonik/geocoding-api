package services

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"geocoding-api/database"

	shp "github.com/jonas-p/go-shp"
	_ "github.com/lib/pq"
)

const enrichSchema = "enrich_probe"

// createBoundaryTables builds the boundary schema the way a deployment does:
// the real migrations, not a copy of their DDL. A copy drifts -- and one that
// left out the GIST index would let the polygon lookup quietly lose it with
// every test still passing.
func createBoundaryTables(t *testing.T, db *sql.DB) {
	t.Helper()
	if err := database.CreateBoundaryTables(db); err != nil {
		t.Fatalf("migration 26: %v", err)
	}
	if err := database.WidenBoundaryGeoIDs(db); err != nil {
		t.Fatalf("migration 27: %v", err)
	}
}

// shapeFeature is one polygon for a fixture shapefile: its rings, and its DBF
// row keyed by column name.
type shapeFeature struct {
	rings [][]shp.Point
	attrs map[string]string
}

func square(x0, y0, x1, y1 float64) []shp.Point {
	// Clockwise, as shapefiles store outer rings.
	return []shp.Point{{X: x0, Y: y0}, {X: x0, Y: y1}, {X: x1, Y: y1}, {X: x1, Y: y0}, {X: x0, Y: y0}}
}

// shapefileZip writes a real shapefile with the library's own writer and zips
// it the way the Census does, so the reader under test sees a genuine file.
func shapefileZip(t *testing.T, columns []string, features []shapeFeature) []byte {
	t.Helper()
	dir := t.TempDir()
	base := filepath.Join(dir, "layer")
	w, err := shp.Create(base+".shp", shp.POLYGON)
	if err != nil {
		t.Fatalf("create shapefile: %v", err)
	}
	fields := make([]shp.Field, len(columns))
	for i, c := range columns {
		fields[i] = shp.StringField(c, 40)
	}
	if err := w.SetFields(fields); err != nil {
		t.Fatalf("set fields: %v", err)
	}
	for _, f := range features {
		p := shp.Polygon(*shp.NewPolyLine(f.rings))
		row := int(w.Write(&p))
		for i, c := range columns {
			if err := w.WriteAttribute(row, i, f.attrs[c]); err != nil {
				t.Fatalf("write attribute: %v", err)
			}
		}
	}
	w.Close()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	// go-shp's writer names the attribute file "layerdbf", dropping the dot
	// (writer.go, SetFields). Only the writer has the bug; the reader under
	// test finds the file by its proper name inside the zip.
	written := map[string]string{".shp": base + ".shp", ".shx": base + ".shx", ".dbf": base + "dbf"}
	for _, ext := range []string{".shp", ".shx", ".dbf"} {
		data, err := os.ReadFile(written[ext])
		if err != nil {
			t.Fatalf("read %s: %v", ext, err)
		}
		fw, _ := zw.Create("tl_fixture" + ext)
		fw.Write(data)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip: %v", err)
	}
	return buf.Bytes()
}

// tigerFixture serves zips by path, 404 for anything else, and can be told
// to fail a path to simulate a broken download.
type tigerFixture struct {
	mu    sync.Mutex
	files map[string][]byte
	fail  map[string]bool
}

func (f *tigerFixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail[r.URL.Path] {
		http.Error(w, "boom", http.StatusInternalServerError)
		return
	}
	data, ok := f.files[r.URL.Path]
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Write(data)
}

func (f *tigerFixture) set(path string, data []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[path] = data
}

func (f *tigerFixture) setFail(path string, fail bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail[path] = fail
}

// Two tracts side by side in "Ohio" (-84..-82, 39..41), the west one with a
// hole -- a lake, in TIGER terms -- that belongs to no tract.
//
//	west  39001000100  -84..-83, with a hole at -83.7..-83.3, 39.7..40.3
//	east  39001000200  -83..-82
var tractColumns = []string{"STATEFP", "COUNTYFP", "TRACTCE", "GEOID", "NAME", "NAMELSAD", "MTFCC"}

func tractFeatures() []shapeFeature {
	hole := square(-83.7, 39.7, -83.3, 40.3)
	// Holes run counter-clockwise; ST_BuildArea does not care, which is the
	// point of using it, but the fixture stays faithful to the format.
	for i, j := 0, len(hole)-1; i < j; i, j = i+1, j-1 {
		hole[i], hole[j] = hole[j], hole[i]
	}
	return []shapeFeature{
		{rings: [][]shp.Point{square(-84, 39, -83, 41), hole}, attrs: map[string]string{
			"STATEFP": "39", "COUNTYFP": "001", "TRACTCE": "000100", "GEOID": "39001000100",
			"NAME": "1", "NAMELSAD": "Census Tract 1", "MTFCC": "G5020"}},
		{rings: [][]shp.Point{square(-83, 39, -82, 41)}, attrs: map[string]string{
			"STATEFP": "39", "COUNTYFP": "001", "TRACTCE": "000200", "GEOID": "39001000200",
			"NAME": "2", "NAMELSAD": "Census Tract 2", "MTFCC": "G5020"}},
	}
}

func setupEnrichDB(t *testing.T) (*sql.DB, *tigerFixture) {
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

	// Registered before any setup can fail, so a failed setup still drops
	// the schema and restores the base URL.
	fixture := &tigerFixture{files: map[string][]byte{}, fail: map[string]bool{}}
	srv := httptest.NewServer(fixture)
	prevURL := tigerBaseURL
	tigerBaseURL = srv.URL
	t.Cleanup(func() {
		srv.Close()
		tigerBaseURL = prevURL
		if _, err := db.Exec(fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", enrichSchema)); err != nil {
			t.Logf("cleanup: %v", err)
		}
		db.Close()
	})

	for _, stmt := range []string{
		"CREATE EXTENSION IF NOT EXISTS postgis",
		fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", enrichSchema),
		fmt.Sprintf("CREATE SCHEMA %s", enrichSchema),
		// public for PostGIS only; every table read is created here.
		fmt.Sprintf("SET search_path TO %s, public", enrichSchema),
		`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TIMESTAMP DEFAULT NOW())`,
		fmt.Sprintf(`INSERT INTO schema_migrations (version) SELECT generate_series(1, %d)`, database.SchemaVersionBoundaryGeoIDs),
		`CREATE TABLE us_states (
			id BIGSERIAL PRIMARY KEY, state_fips VARCHAR(2) NOT NULL UNIQUE,
			state_abbr VARCHAR(2) NOT NULL UNIQUE, state_name VARCHAR(255) NOT NULL UNIQUE,
			geometry GEOMETRY(MULTIPOLYGON, 4326))`,
		// Kentucky is a triangle, not an envelope, so a feature can sit
		// inside its bounds and outside the state -- which is the difference
		// between the two filters a national layer passes through.
		`INSERT INTO us_states (state_fips, state_abbr, state_name, geometry) VALUES
		 ('39','OH','Ohio',   ST_Multi(ST_MakeEnvelope(-84.8,38.4,-80.5,42.0,4326))),
		 ('18','IN','Indiana',ST_Multi(ST_MakeEnvelope(-88.1,37.8,-84.8,41.8,4326))),
		 ('21','KY','Kentucky',ST_Multi(ST_GeomFromText(
			'POLYGON((-89 33, -84 33, -89 37, -89 33))', 4326)))`,
		// Reverse reads these; empty is enough, since what is tested here is
		// the timezone it reports.
		`CREATE TABLE ohio_addresses (
			id BIGSERIAL PRIMARY KEY, hash VARCHAR(255) NOT NULL,
			house_number VARCHAR(50), street VARCHAR(255), unit VARCHAR(50),
			city VARCHAR(255), district VARCHAR(10), region VARCHAR(2) NOT NULL,
			postcode VARCHAR(10), county VARCHAR(255),
			geom GEOMETRY(POINT, 4326) NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP, full_address TEXT)`,
		`CREATE TABLE ohio_counties (
			id BIGSERIAL PRIMARY KEY, county_name VARCHAR(255) NOT NULL,
			bounds_geometry GEOMETRY(MULTIPOLYGON, 4326))`,
		// ZIPs for the timezone field. geog as migration 21 generates it.
		`CREATE TABLE zip_codes (
			zip_code VARCHAR(10) PRIMARY KEY, state_code VARCHAR(2) NOT NULL,
			timezone VARCHAR(100) NOT NULL, latitude DOUBLE PRECISION, longitude DOUBLE PRECISION,
			geog geography(Point,4326) GENERATED ALWAYS AS
				(ST_SetSRID(ST_MakePoint(longitude, latitude), 4326)::geography) STORED)`,
		`INSERT INTO zip_codes (zip_code, state_code, timezone, latitude, longitude) VALUES
		 ('43215','OH','America/New_York',39.96,-83.00)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("setup failed on %.60q: %v", stmt, err)
		}
	}
	createBoundaryTables(t, db)
	return db, fixture
}

func mustLayer(t *testing.T, name string) BoundaryLayer {
	t.Helper()
	l, ok := BoundaryLayerByName(name)
	if !ok {
		t.Fatalf("no layer %s", name)
	}
	return l
}

func load(t *testing.T, db *sql.DB, layer, state string) (int, error) {
	t.Helper()
	claim, err := BeginBoundaryLoad(db, mustLayer(t, layer), state)
	if err != nil {
		return 0, err
	}
	return claim.Load(context.Background(), db)
}

func loadRow(t *testing.T, db *sql.DB, layer, state string) (status string, available bool, features int) {
	t.Helper()
	if err := db.QueryRow(`SELECT status, available, features FROM boundary_loads WHERE layer = $1 AND state_fips = $2`,
		layer, state).Scan(&status, &available, &features); err != nil {
		t.Fatalf("boundary_loads %s/%s: %v", layer, state, err)
	}
	return
}

func enrichAt(t *testing.T, db *sql.DB, lat, lng float64, fields string) *Enrichment {
	t.Helper()
	req, err := ParseEnrichmentFields(fields)
	if err != nil {
		t.Fatalf("fields %q: %v", fields, err)
	}
	e, err := Enrich(db, lat, lng, req)
	if err != nil {
		t.Fatalf("enrich: %v", err)
	}
	return e
}

func TestEnrichFindsTheContainingTract(t *testing.T) {
	db, fx := setupEnrichDB(t)
	fx.set("/TRACT/tl_2025_39_tract.zip", shapefileZip(t, tractColumns, tractFeatures()))

	n, err := load(t, db, "tract", "39")
	if err != nil || n != 2 {
		t.Fatalf("load: %d features, %v", n, err)
	}
	if status, available, features := loadRow(t, db, "tract", "39"); status != "loaded" || !available || features != 2 {
		t.Errorf("boundary_loads = %s available=%v features=%d", status, available, features)
	}

	e := enrichAt(t, db, 40.5, -83.9, "census")
	tract := e.Boundaries["tract"]
	if tract == nil || tract.GEOID != "39001000100" || tract.Name != "Census Tract 1" {
		t.Fatalf("tract = %+v", tract)
	}
	if tract.Attributes["county_fips"] != "001" || tract.Attributes["tract_code"] != "000100" {
		t.Errorf("attributes = %v", tract.Attributes)
	}
	if _, stored := tract.Attributes["MTFCC"]; stored {
		t.Error("an unlisted DBF column was stored")
	}
	if e.StateFIPS == nil || *e.StateFIPS != "39" {
		t.Errorf("state_fips = %v", e.StateFIPS)
	}

	// Inside the west tract's hole: the layer is loaded and the point is in
	// no tract. That is an answer (null), not a gap (unavailable).
	lake := enrichAt(t, db, 40.0, -83.5, "census")
	if b, present := lake.Boundaries["tract"]; !present || b != nil {
		t.Errorf("point in the hole: tract = %+v (present=%v), want an explicit null", b, present)
	}
	for _, u := range lake.Unavailable {
		if u == "tract" {
			t.Error("a loaded layer was reported unavailable")
		}
	}
}

// "Not loaded" and "none here" must never read the same.
func TestEnrichSeparatesUnloadedFromNone(t *testing.T) {
	db, fx := setupEnrichDB(t)
	fx.set("/TRACT/tl_2025_39_tract.zip", shapefileZip(t, tractColumns, tractFeatures()))
	if _, err := load(t, db, "tract", "39"); err != nil {
		t.Fatal(err)
	}
	// No elementary-district file exists for Ohio: a 404 is "absent".
	if n, err := load(t, db, "school_elementary", "39"); err != nil || n != 0 {
		t.Fatalf("absent layer: %d, %v", n, err)
	}
	if status, available, _ := loadRow(t, db, "school_elementary", "39"); status != "absent" || !available {
		t.Errorf("absent layer recorded as %s available=%v", status, available)
	}

	e := enrichAt(t, db, 40.5, -83.9, "census,school")
	if e.Boundaries["tract"] == nil {
		t.Error("tract missing")
	}
	if b, present := e.Boundaries["school_elementary"]; !present || b != nil {
		t.Errorf("absent layer should be an explicit null, got %+v present=%v", b, present)
	}
	unavailable := strings.Join(e.Unavailable, ",")
	for _, want := range []string{"block_group", "block", "school_unified", "school_secondary"} {
		if !strings.Contains(unavailable, want) {
			t.Errorf("%s was never loaded but is not unavailable (%s)", want, unavailable)
		}
		if _, present := e.Boundaries[want]; present {
			t.Errorf("unloaded %s has a boundaries entry", want)
		}
	}

	// Indiana: tracts are loaded for Ohio only.
	in := enrichAt(t, db, 40.0, -86.0, "census")
	if !strings.Contains(strings.Join(in.Unavailable, ","), "tract") {
		t.Errorf("Indiana tracts reported as available: %+v", in)
	}

	// Open water: no state, so nothing can have been loaded for it.
	sea := enrichAt(t, db, 30.0, -60.0, "census")
	if sea.StateFIPS != nil || len(sea.Unavailable) != 0 || sea.Boundaries["tract"] != nil {
		t.Errorf("point in no state: %+v", sea)
	}
}

// A reload that fails rolls back; the previous load keeps serving and is not
// reported unavailable just because the latest attempt went wrong.
func TestFailedReloadKeepsThePreviousLoadServing(t *testing.T) {
	db, fx := setupEnrichDB(t)
	path := "/TRACT/tl_2025_39_tract.zip"
	fx.set(path, shapefileZip(t, tractColumns, tractFeatures()))
	if _, err := load(t, db, "tract", "39"); err != nil {
		t.Fatal(err)
	}

	fx.setFail(path, true)
	if _, err := load(t, db, "tract", "39"); err == nil {
		t.Fatal("a 500 from the Census loaded without error")
	}
	status, available, features := loadRow(t, db, "tract", "39")
	if status != "failed" || !available || features != 2 {
		t.Errorf("after a failed reload: %s available=%v features=%d", status, available, features)
	}
	if e := enrichAt(t, db, 40.5, -83.9, "census"); e.Boundaries["tract"] == nil {
		t.Error("the previous load stopped serving after a failed reload")
	}

	// A corrupt file fails mid-transaction, after the DELETE: the rollback
	// has to bring the old rows back.
	fx.setFail(path, false)
	fx.set(path, []byte("not a zip"))
	if _, err := load(t, db, "tract", "39"); err == nil {
		t.Fatal("a corrupt file loaded without error")
	}
	var rows int
	db.QueryRow(`SELECT count(*) FROM boundaries WHERE layer = 'tract'`).Scan(&rows)
	if rows != 2 {
		t.Errorf("%d tract rows after a failed load, want the previous 2", rows)
	}
}

func TestReloadReplacesRatherThanAppends(t *testing.T) {
	db, fx := setupEnrichDB(t)
	path := "/TRACT/tl_2025_39_tract.zip"
	fx.set(path, shapefileZip(t, tractColumns, tractFeatures()))
	if _, err := load(t, db, "tract", "39"); err != nil {
		t.Fatal(err)
	}
	// The new vintage drops the east tract.
	fx.set(path, shapefileZip(t, tractColumns, tractFeatures()[:1]))
	if n, err := load(t, db, "tract", "39"); err != nil || n != 1 {
		t.Fatalf("reload: %d, %v", n, err)
	}
	if e := enrichAt(t, db, 40.0, -82.5, "census"); e.Boundaries["tract"] != nil {
		t.Errorf("a tract dropped from the file still answers: %+v", e.Boundaries["tract"])
	}
}

func TestBoundaryLoadClaims(t *testing.T) {
	db, fx := setupEnrichDB(t)
	fx.set("/TRACT/tl_2025_39_tract.zip", shapefileZip(t, tractColumns, tractFeatures()))
	tract := mustLayer(t, "tract")

	first, err := BeginBoundaryLoad(db, tract, "39")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := BeginBoundaryLoad(db, tract, "39"); !errors.Is(err, ErrBoundaryLoadInProgress) {
		t.Errorf("second concurrent claim: %v, want ErrBoundaryLoadInProgress", err)
	}
	// Other states and layers are independent.
	if _, err := BeginBoundaryLoad(db, tract, "18"); err != nil {
		t.Errorf("a different state was blocked: %v", err)
	}

	// The first load stalls past the stale window and its claim is taken.
	db.Exec(`UPDATE boundary_loads SET started_at = NOW() - interval '2 hours' WHERE layer = 'tract' AND state_fips = '39'`)
	second, err := BeginBoundaryLoad(db, tract, "39")
	if err != nil {
		t.Fatalf("a stale claim still blocks: %v", err)
	}
	fx.setFail("/TRACT/tl_2025_39_tract.zip", true)
	if _, err := second.Load(context.Background(), db); err == nil {
		t.Fatal("the second load should fail against a 500")
	}
	fx.setFail("/TRACT/tl_2025_39_tract.zip", false)

	// The stalled first load finishes late. Its success must not overwrite
	// the outcome of the load that replaced it.
	if _, err := first.Load(context.Background(), db); err == nil || !strings.Contains(err.Error(), "taken over") {
		t.Errorf("a load whose claim was taken over reported: %v", err)
	}
	if status, _, _ := loadRow(t, db, "tract", "39"); status != "failed" {
		t.Errorf("status = %s; the late load overwrote the newer outcome", status)
	}

	if _, err := BeginBoundaryLoad(db, tract, "3"); err == nil {
		t.Error("a malformed FIPS code was accepted")
	}
}

// A 404 for a layer that was loaded means the file moved, not that the
// districts vanished. The rows must survive, and must not be recorded as a
// trusted "none".
func TestA404AfterALoadKeepsTheRows(t *testing.T) {
	db, fx := setupEnrichDB(t)
	path := "/TRACT/tl_2025_39_tract.zip"
	fx.set(path, shapefileZip(t, tractColumns, tractFeatures()))
	if _, err := load(t, db, "tract", "39"); err != nil {
		t.Fatal(err)
	}
	fx.mu.Lock()
	delete(fx.files, path)
	fx.mu.Unlock()

	if _, err := load(t, db, "tract", "39"); err == nil || !strings.Contains(err.Error(), "keeping the 2") {
		t.Errorf("404 after a load: %v", err)
	}
	status, available, features := loadRow(t, db, "tract", "39")
	if status != "failed" || !available || features != 2 {
		t.Errorf("after a 404: %s available=%v features=%d", status, available, features)
	}
	if e := enrichAt(t, db, 40.5, -83.9, "census"); e.Boundaries["tract"] == nil {
		t.Error("tracts stopped answering after a 404")
	}
}

func TestEnrichTimezoneAndSource(t *testing.T) {
	db, _ := setupEnrichDB(t)
	e := enrichAt(t, db, 39.97, -83.01, "timezone")
	if e.Timezone == nil || *e.Timezone != "America/New_York" {
		t.Errorf("timezone = %v", e.Timezone)
	}
	if e.Source != "Census TIGER/Line 2025" {
		t.Errorf("source = %q", e.Source)
	}
	if len(e.Boundaries) != 0 || len(e.Unavailable) != 0 {
		t.Errorf("fields=timezone returned boundaries: %+v", e)
	}
	if e := enrichAt(t, db, 39.97, -83.01, "cd"); e.Timezone != nil || strings.Contains(strings.Join(e.Unavailable, ","), "timezone") {
		t.Error("timezone reported when not asked for")
	}
	// Asked for with no ZIP near: unknown, and said so.
	far := enrichAt(t, db, 41.5, -86.0, "timezone")
	if far.Timezone != nil || strings.Join(far.Unavailable, ",") != "timezone" {
		t.Errorf("timezone with no ZIP data: %v, unavailable %v", far.Timezone, far.Unavailable)
	}
}

// Census block files suffix every column with the vintage.
func TestBlockColumnsWithVintageSuffix(t *testing.T) {
	db, fx := setupEnrichDB(t)
	cols := []string{"STATEFP20", "COUNTYFP20", "TRACTCE20", "BLOCKCE20", "GEOID20", "NAME20"}
	fx.set("/TABBLOCK20/tl_2025_39_tabblock20.zip", shapefileZip(t, cols, []shapeFeature{
		{rings: [][]shp.Point{square(-84, 39, -83, 41)}, attrs: map[string]string{
			"STATEFP20": "39", "COUNTYFP20": "001", "TRACTCE20": "000100", "BLOCKCE20": "1001",
			"GEOID20": "390010001001001", "NAME20": "Block 1001"}},
	}))
	if _, err := load(t, db, "block", "39"); err != nil {
		t.Fatal(err)
	}
	b := enrichAt(t, db, 40.5, -83.9, "census").Boundaries["block"]
	if b == nil || b.GEOID != "390010001001001" || b.Name != "Block 1001" || b.Attributes["block_code"] != "1001" {
		t.Errorf("block = %+v", b)
	}
}

// A point on the shared edge is covered by both tracts; the answer must not
// depend on which row the index returns first.
func TestSharedEdgeIsDeterministic(t *testing.T) {
	db, fx := setupEnrichDB(t)
	fx.set("/TRACT/tl_2025_39_tract.zip", shapefileZip(t, tractColumns, tractFeatures()))
	if _, err := load(t, db, "tract", "39"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if b := enrichAt(t, db, 40.5, -83.0, "census").Boundaries["tract"]; b == nil || b.GEOID != "39001000100" {
			t.Fatalf("edge point: %+v, want the lower GEOID", b)
		}
	}
}

func TestParseEnrichmentFields(t *testing.T) {
	// Every layer but the timezone one, which answers through Timezone.
	boundaryLayers := 0
	for _, l := range BoundaryLayers {
		if l.Group != "timezone" {
			boundaryLayers++
		}
	}
	all, err := ParseEnrichmentFields("")
	if err != nil || len(all.Layers) != boundaryLayers || !all.Timezone {
		t.Errorf("empty fields: %d layers, timezone=%v, %v", len(all.Layers), all.Timezone, err)
	}
	cd, err := ParseEnrichmentFields(" CD , ")
	if err != nil || len(cd.Layers) != 1 || cd.Layers[0].Name != "congressional_district" || cd.Timezone {
		t.Errorf("cd: %+v, %v", cd, err)
	}
	if _, err := ParseEnrichmentFields("census,zodiac"); err == nil || !strings.Contains(err.Error(), "zodiac") {
		t.Errorf("unknown field: %v", err)
	}
}

// Part offsets come from the file; a corrupt one must be an error, not a
// panic in a goroutine the server cannot recover.
func TestRingsWKTRejectsCorruptParts(t *testing.T) {
	ring := square(0, 0, 1, 1)
	cases := map[string]shp.Polygon{
		"offset past the points": {NumParts: 2, Parts: []int32{0, 99}, Points: ring},
		"negative offset":        {NumParts: 1, Parts: []int32{-1}, Points: ring},
		"parts count mismatch":   {NumParts: 3, Parts: []int32{0}, Points: ring},
		"no area":                {NumParts: 1, Parts: []int32{0}, Points: ring[:3]},
	}
	for name, p := range cases {
		p := p
		if _, err := ringsWKT(&p); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// A degenerate ring beside a real one is dropped, not fatal.
	ok := shp.Polygon{NumParts: 2, Parts: []int32{0, 5}, Points: append(append([]shp.Point{}, ring...), ring[:3]...)}
	if wkt, err := ringsWKT(&ok); err != nil || strings.Count(wkt, "(") != 2 {
		t.Errorf("degenerate ring beside a real one: %q, %v", wkt, err)
	}
}

// zoneColumns is the timezone shapefile's single field.
var zoneColumns = []string{"tzid"}

// The fixture splits "Indiana" at -87.53 the way the real Indiana is split,
// and puts a zone in West Africa to be filtered out.
func zoneFeatures() []shapeFeature {
	return []shapeFeature{
		{rings: [][]shp.Point{square(-89, 37, -87.53, 42)}, attrs: map[string]string{"tzid": "America/Chicago"}},
		{rings: [][]shp.Point{square(-87.53, 37, -84.8, 42)}, attrs: map[string]string{"tzid": "America/Indiana/Vincennes"}},
		{rings: [][]shp.Point{square(-5, 4, 0, 6)}, attrs: map[string]string{"tzid": "Africa/Abidjan"}},
		// Inside Kentucky's bounding box, outside Kentucky: rejected by
		// ST_Intersects after the cheap bounds filter lets it through.
		{rings: [][]shp.Point{square(-85, 36.5, -84.5, 36.9)}, attrs: map[string]string{"tzid": "America/Nowhere"}},
	}
}

// serveZones points the timezone layer at the fixture server for this test.
// It mutates the package-level registry, which is safe only because none of
// these tests call t.Parallel.
func serveZones(t *testing.T, fx *tigerFixture) {
	t.Helper()
	fx.set("/timezones.shapefile.zip", shapefileZip(t, zoneColumns, zoneFeatures()))
	for i := range BoundaryLayers {
		if BoundaryLayers[i].Name == "timezone" {
			prev := BoundaryLayers[i].URL
			BoundaryLayers[i].URL = tigerBaseURL + "/timezones.shapefile.zip"
			t.Cleanup(func() { BoundaryLayers[i].URL = prev })
			return
		}
	}
	t.Fatal("no timezone layer")
}

// The zone polygons put the line where the line is, including lines that run
// through a state, which the nearest-ZIP fallback cannot.
func TestTimezoneBoundariesAnswerExactly(t *testing.T) {
	db, fx := setupEnrichDB(t)
	serveZones(t, fx)

	n, err := load(t, db, "timezone", "39")
	if err != nil {
		t.Fatal(err)
	}
	// Two of the four zones touch a state. The West African one is rejected
	// by the bounds filter; America/Nowhere clears that filter, sitting
	// inside Kentucky's bounding box, and is rejected by ST_Intersects.
	if n != 2 {
		t.Fatalf("kept %d zones, want the 2 that touch a state", n)
	}
	var kept []string
	rows, err := db.Query(`SELECT geoid FROM boundaries WHERE layer = 'timezone' ORDER BY geoid`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var z string
		if err := rows.Scan(&z); err != nil {
			t.Fatal(err)
		}
		kept = append(kept, z)
	}
	rows.Close()
	if strings.Join(kept, ",") != "America/Chicago,America/Indiana/Vincennes" {
		t.Errorf("kept %v", kept)
	}
	// National: loaded once, recorded against NationalScope rather than the
	// state that happened to ask for it.
	if status, available, features := loadRow(t, db, "timezone", NationalScope); status != "loaded" || !available || features != 2 {
		t.Errorf("boundary_loads = %s available=%v features=%d", status, available, features)
	}

	// Two points a few kilometres apart inside one state, either side of the
	// line. The ZIP fixture only knows America/New_York, so a ZIP-derived
	// answer could not tell these apart.
	east := enrichAt(t, db, 39.58, -87.52, "timezone")
	if east.Timezone == nil || *east.Timezone != "America/Indiana/Vincennes" {
		t.Errorf("east of the line: %v", east.Timezone)
	}
	if east.TimezoneSource != TimezoneBoundarySource {
		t.Errorf("timezone_source = %q, want the boundary source", east.TimezoneSource)
	}
	west := enrichAt(t, db, 39.58, -87.60, "timezone")
	if west.Timezone == nil || *west.Timezone != "America/Chicago" {
		t.Errorf("west of the line: %v", west.Timezone)
	}

	// Reverse reports the same zone, and says where it came from.
	rev, err := ReverseGeocode(db, 39.58, -87.52, 0)
	if err != nil {
		t.Fatal(err)
	}
	if rev.Timezone == nil || *rev.Timezone != "America/Indiana/Vincennes" || rev.TimezoneSource != TimezoneBoundarySource {
		t.Errorf("reverse: %v from %q", rev.Timezone, rev.TimezoneSource)
	}
}

// Without the layer, the ZIP fallback still answers, and says it is the one
// answering.
func TestTimezoneFallsBackToZipCentroids(t *testing.T) {
	db, fx := setupEnrichDB(t)

	e := enrichAt(t, db, 39.97, -83.01, "timezone")
	if e.Timezone == nil || *e.Timezone != "America/New_York" || e.TimezoneSource != TimezoneZIPSource {
		t.Errorf("without the layer: %v from %q", e.Timezone, e.TimezoneSource)
	}

	// With the layer loaded but the point outside every zone -- the polygons
	// stop at the coast -- the fallback answers rather than nothing.
	serveZones(t, fx)
	if _, err := load(t, db, "timezone", "39"); err != nil {
		t.Fatal(err)
	}
	sea := enrichAt(t, db, 39.97, -83.01, "timezone")
	if sea.Timezone == nil || *sea.Timezone != "America/New_York" || sea.TimezoneSource != TimezoneZIPSource {
		t.Errorf("outside every zone: %v from %q", sea.Timezone, sea.TimezoneSource)
	}
}

// A national layer is loaded once, not once per state, so a routine load of a
// state must not re-fetch 80 MB of zones.
func TestNationalLayersAreNotInTheDefaultLoad(t *testing.T) {
	for _, l := range DefaultBoundaryLayers() {
		if l.URL != "" {
			t.Errorf("national layer %s is in the default load", l.Name)
		}
	}
	tz, ok := BoundaryLayerByName("timezone")
	if !ok || tz.Scope("39") != NationalScope {
		t.Errorf("timezone layer scope = %q", tz.Scope("39"))
	}
	// It is still loadable by name, which is how it gets loaded at all.
	if _, ok := BoundaryLayerByName("timezone"); !ok {
		t.Error("the timezone layer cannot be named in a load")
	}
}
