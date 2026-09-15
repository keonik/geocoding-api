package services

import (
	"database/sql"
	"fmt"
	"math"
	"os"
	"testing"

	_ "github.com/lib/pq"
)

const tileSchema = "tile_probe"

func setupTileDB(t *testing.T) *sql.DB {
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
		fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", tileSchema),
		fmt.Sprintf("CREATE SCHEMA %s", tileSchema),
		fmt.Sprintf("SET search_path TO %s, public", tileSchema),
		`CREATE TABLE ohio_counties (
			id BIGSERIAL PRIMARY KEY,
			county_name VARCHAR(255) NOT NULL,
			bounds_geometry GEOMETRY(MULTIPOLYGON, 4326),
			bounds_geometry_simplified GEOMETRY)`,
		`CREATE TABLE us_states (
			id BIGSERIAL PRIMARY KEY,
			state_fips VARCHAR(2) NOT NULL UNIQUE,
			state_abbr VARCHAR(2) NOT NULL UNIQUE,
			state_name VARCHAR(255) NOT NULL UNIQUE,
			geometry GEOMETRY(MULTIPOLYGON, 4326),
			geometry_simplified GEOMETRY)`,
		// A Franklin County box around Columbus, and Ohio around it.
		`INSERT INTO ohio_counties (county_name, bounds_geometry, bounds_geometry_simplified) VALUES
		 ('Franklin', ST_Multi(ST_MakeEnvelope(-83.2,39.8,-82.8,40.2,4326)),
		              ST_Multi(ST_MakeEnvelope(-83.2,39.8,-82.8,40.2,4326)))`,
		`INSERT INTO us_states (state_fips, state_abbr, state_name, geometry, geometry_simplified) VALUES
		 ('39','OH','Ohio', ST_Multi(ST_MakeEnvelope(-85.0,38.4,-80.5,42.0,4326)),
		                    ST_Multi(ST_MakeEnvelope(-85.0,38.4,-80.5,42.0,4326)))`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("setup failed on %.60q: %v", stmt, err)
		}
	}

	t.Cleanup(func() {
		if _, err := db.Exec(fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", tileSchema)); err != nil {
			t.Logf("cleanup: %v", err)
		}
		db.Close()
	})
	return db
}

// tileFor returns the z/x/y covering a longitude/latitude at a zoom, using the
// standard slippy-map formulas, so the test asks for the tile the point is
// genuinely in rather than one found by trial and error.
func tileFor(lng, lat float64, z int) (int, int) {
	n := float64(int(1) << uint(z))
	x := int((lng + 180.0) / 360.0 * n)
	// The Mercator y formula, written out rather than imported for one use.
	latRad := lat * 3.141592653589793 / 180.0
	ys := (1.0 - logTan(latRad)) / 2.0 * n
	return x, int(ys)
}

func logTan(latRad float64) float64 {
	// ln(tan(lat) + sec(lat)) / pi
	sin := sinApprox(latRad)
	return 0.5 * logApprox((1+sin)/(1-sin)) / 3.141592653589793
}

func sinApprox(x float64) float64 { return math.Sin(x) }
func logApprox(x float64) float64 { return math.Log(x) }

// Columbus sits in Franklin County, so a tile covering it must carry a feature.
func TestTileContainsTheFeatureCoveringThePoint(t *testing.T) {
	db := setupTileDB(t)

	z := 8
	x, y := tileFor(-83.0, 40.0, z)

	tile, err := RenderTile(db, TileLayers["counties"], z, x, y)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if tile == nil {
		t.Fatalf("tile %d/%d/%d over Columbus is empty; Franklin County should be in it", z, x, y)
	}
	// A Mapbox Vector Tile is protobuf; the layer name appears in it verbatim.
	if !containsBytes(tile, []byte("counties")) {
		t.Error("the tile does not name the counties layer")
	}
	if !containsBytes(tile, []byte("Franklin")) {
		t.Error("the tile does not carry the county name, so a renderer cannot label it")
	}
	t.Logf("tile %d/%d/%d: %d bytes", z, x, y, len(tile))
}

// Most tiles in a pyramid are empty. Answering 204 rather than an empty
// encoding saves every client parsing a tile with nothing in it.
func TestEmptyTileIsNil(t *testing.T) {
	db := setupTileDB(t)

	// Mid-Pacific at zoom 8.
	tile, err := RenderTile(db, TileLayers["counties"], 8, 20, 120)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if tile != nil {
		t.Errorf("a tile far from any county returned %d bytes, want nil", len(tile))
	}
}

// Both layers render, and they carry different features.
func TestBothLayersRender(t *testing.T) {
	db := setupTileDB(t)

	z := 6
	x, y := tileFor(-83.0, 40.0, z)

	counties, err := RenderTile(db, TileLayers["counties"], z, x, y)
	if err != nil {
		t.Fatalf("counties: %v", err)
	}
	states, err := RenderTile(db, TileLayers["states"], z, x, y)
	if err != nil {
		t.Fatalf("states: %v", err)
	}

	if counties == nil || states == nil {
		t.Fatalf("expected both layers over Ohio, got counties=%v states=%v", counties != nil, states != nil)
	}
	if !containsBytes(states, []byte("OH")) {
		t.Error("the states tile does not carry the state code")
	}
}

// A request for a tile that does not exist must be rejected, not answered with
// an empty 200 the client will cache and keep retrying.
func TestTileCoordinateValidation(t *testing.T) {
	cases := []struct {
		name    string
		z, x, y int
		wantErr bool
	}{
		{"origin", 0, 0, 0, false},
		{"valid mid zoom", 8, 70, 97, false},
		{"zoom too deep", MaxTileZoom + 1, 0, 0, true},
		{"negative zoom", -1, 0, 0, true},
		{"x past the grid", 2, 4, 0, true},
		{"y past the grid", 2, 0, 4, true},
		{"negative x", 2, -1, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidTileCoordinates(tc.z, tc.x, tc.y)
			if tc.wantErr && err == nil {
				t.Errorf("(%d,%d,%d) was accepted", tc.z, tc.x, tc.y)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("(%d,%d,%d) was rejected: %v", tc.z, tc.x, tc.y, err)
			}
		})
	}
}

func containsBytes(haystack, needle []byte) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
