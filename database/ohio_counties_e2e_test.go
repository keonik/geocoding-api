package database

import (
	"database/sql"
	"os"
	"testing"

	_ "github.com/lib/pq"
)

// End-to-end: fetch the county sidecars from Ohio's live ArcGIS services, then
// load them. Hits the network, so it is opt-in via PROBE_DSN like the other
// probes. Runs from the repo root because the loader globs a relative oh/.
func TestLoadOhioCountyBoundariesE2E(t *testing.T) {
	dsn := os.Getenv("PROBE_DSN")
	if dsn == "" {
		t.Skip("PROBE_DSN not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	var exists bool
	if err := db.QueryRow(`SELECT to_regclass('ohio_counties') IS NOT NULL`).Scan(&exists); err != nil || !exists {
		t.Skip("probe database has no ohio_counties table")
	}

	wd, _ := os.Getwd()
	if err := os.Chdir(".."); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(wd)

	prev := DB
	DB = db
	defer func() { DB = prev }()

	loaded, err := LoadOhioCountyBoundaries()
	if err != nil {
		t.Fatalf("LoadOhioCountyBoundaries: %v", err)
	}
	if loaded < 80 {
		t.Fatalf("loaded %d counties, expected all 88", loaded)
	}
	t.Logf("loaded %d counties", loaded)

	// A real outline, not a bounding box: Franklin should have hundreds of
	// vertices and sit inside its own known extent.
	var pts int
	var xmin, ymin, xmax, ymax float64
	var addrs int
	err = db.QueryRow(`
		SELECT ST_NPoints(bounds_geometry), ST_XMin(bounds_geometry), ST_YMin(bounds_geometry),
		       ST_XMax(bounds_geometry), ST_YMax(bounds_geometry), address_count
		FROM ohio_counties WHERE county_name = 'Franklin'`).
		Scan(&pts, &xmin, &ymin, &xmax, &ymax, &addrs)
	if err != nil {
		t.Fatalf("Franklin: %v", err)
	}
	t.Logf("Franklin: %d vertices, bbox (%.4f,%.4f)..(%.4f,%.4f), %d addresses", pts, xmin, ymin, xmax, ymax, addrs)

	if pts < 100 {
		t.Errorf("Franklin has %d vertices; a bounding box would have 5", pts)
	}
	if xmin < -84.0 || xmax > -82.5 || ymin < 39.6 || ymax > 40.3 {
		t.Errorf("Franklin bbox outside its known extent: (%.4f,%.4f)..(%.4f,%.4f)", xmin, ymin, xmax, ymax)
	}
	if addrs < 100000 {
		t.Errorf("Franklin address_count = %d, expected ~699k", addrs)
	}

	// Adams is the county whose address-point extent was wildly wrong; its
	// real outline must be small and southern.
	var aymax float64
	if err := db.QueryRow(`SELECT ST_YMax(bounds_geometry) FROM ohio_counties WHERE county_name='Adams'`).Scan(&aymax); err != nil {
		t.Fatalf("Adams: %v", err)
	}
	if aymax > 39.3 {
		t.Errorf("Adams extends to lat %.4f; point-extent bug would give ~40.92", aymax)
	}
	t.Logf("Adams northern edge: %.4f (point-extent gave 40.92)", aymax)
}
