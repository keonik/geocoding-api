package services

import (
	"fmt"
	"strings"
	"testing"

	"geocoding-api/models"
)

// A bbox is the viewport case: a map is showing a rectangle and wants the
// addresses inside it.
func TestBBoxRestrictsToTheRectangle(t *testing.T) {
	db := setupCountTestDB(t)
	svc := NewAddressService(db)

	// The fixture walks points along a diagonal from (-84.0, 39.0), one
	// thousandth of a degree per row. This box covers the first hundred.
	box := &models.BoundingBox{MinLng: -84.101, MinLat: 39.0, MaxLng: -84.0, MaxLat: 39.101}

	rows, total, err := svc.SearchAddresses(models.AddressSearchParams{BBox: box, Limit: 500})
	if err != nil {
		t.Fatalf("bbox search: %v", err)
	}
	if total == 0 {
		t.Fatal("no rows inside a box that covers part of the fixture")
	}

	// Every returned point must actually be inside. A bbox that silently
	// widens is worse than one that errors: the caller gets plausible rows
	// from the wrong place.
	for _, r := range rows {
		if r.Longitude < box.MinLng || r.Longitude > box.MaxLng ||
			r.Latitude < box.MinLat || r.Latitude > box.MaxLat {
			t.Errorf("%s at (%f, %f) is outside the requested box",
				r.FullAddress, r.Longitude, r.Latitude)
		}
	}

	// And it must actually restrict -- an unbounded search returns more.
	_, allTotal, err := svc.SearchAddresses(models.AddressSearchParams{Limit: 1})
	if err != nil {
		t.Fatalf("unbounded search: %v", err)
	}
	if total >= allTotal {
		t.Errorf("bbox returned %d of %d rows; it is not restricting anything", total, allTotal)
	}
	t.Logf("bbox selected %d of %d addresses", total, allTotal)
}

// The point of a polygon is the shapes a rectangle cannot express. The fixture
// seeds its points along a perfect diagonal, so any box around a stretch of it
// and any band following it select the same rows -- this test adds points that
// sit inside the box but off the line, which is what a real territory has to
// exclude.
func TestPolygonExcludesWhatItsBoundingBoxWouldInclude(t *testing.T) {
	db := setupCountTestDB(t)
	svc := NewAddressService(db)

	// Two points well off the diagonal but comfortably inside the box below.
	if _, err := db.Exec(`
		INSERT INTO ohio_addresses (hash, house_number, street, unit, city, district, region, postcode, county, geom, full_address)
		VALUES
		  ('off-diagonal-a', '1', 'Off Line Road', '', 'Columbus', 'FRA', 'OH', '43004', 'Franklin',
		   ST_SetSRID(ST_MakePoint(-84.045, 39.005), 4326), '1 Off Line Road, Columbus, OH 43004'),
		  ('off-diagonal-b', '2', 'Off Line Road', '', 'Columbus', 'FRA', 'OH', '43004', 'Franklin',
		   ST_SetSRID(ST_MakePoint(-84.005, 39.045), 4326), '2 Off Line Road, Columbus, OH 43004')
	`); err != nil {
		t.Fatalf("seed off-diagonal points: %v", err)
	}

	box := &models.BoundingBox{MinLng: -84.0505, MinLat: 38.9995, MaxLng: -83.9995, MaxLat: 39.0505}

	// A narrow band hugging the diagonal. Its bounding box is the square
	// above; the band itself is a few thousandths of a degree wide, so the two
	// corner points fall outside it.
	polygon := `{"type":"Polygon","coordinates":[[
		[-84.0000,38.9970],[-84.0530,39.0500],[-84.0500,39.0530],[-83.9970,39.0000],[-84.0000,38.9970]
	]]}`

	_, boxTotal, err := svc.SearchAddresses(models.AddressSearchParams{BBox: box, Limit: 1})
	if err != nil {
		t.Fatalf("bbox search: %v", err)
	}
	rows, polyTotal, err := svc.SearchAddresses(models.AddressSearchParams{Polygon: polygon, Limit: 500})
	if err != nil {
		t.Fatalf("polygon search: %v", err)
	}

	t.Logf("bounding box: %d addresses, polygon: %d", boxTotal, polyTotal)
	if polyTotal == 0 {
		t.Fatal("the band follows the fixture's diagonal and should contain its points")
	}

	// Assert on what the polygon excluded, not on a total that happens to be
	// one smaller. The band's true bounding box is slightly wider than `box`,
	// so it picks up a diagonal point `box` misses -- comparing totals cleared
	// by a single row, and any fixture change would have flipped it into a
	// failure claiming the polygon was not narrowing anything when it was.
	var offLine int
	for _, r := range rows {
		if r.Street == "Off Line Road" {
			offLine++
			t.Errorf("%s sits off the band and should have been excluded", r.FullAddress)
		}
	}

	// The two seeded points are inside the rectangle by construction, so the
	// rectangle must return them and the band must not.
	boxRows, _, err := svc.SearchAddresses(models.AddressSearchParams{BBox: box, Limit: 500})
	if err != nil {
		t.Fatalf("bbox rows: %v", err)
	}
	var offLineInBox int
	for _, r := range boxRows {
		if r.Street == "Off Line Road" {
			offLineInBox++
		}
	}
	if offLineInBox != 2 {
		t.Errorf("the rectangle returned %d off-diagonal points, want 2 -- the fixture no longer sets up the contrast", offLineInBox)
	}
	if offLine != 0 {
		t.Errorf("the polygon returned %d off-diagonal points, want 0", offLine)
	}
}

// Territory filters have to combine with the rest of the query, or they are a
// separate endpoint wearing the same URL.
func TestBBoxCombinesWithTextSearch(t *testing.T) {
	db := setupCountTestDB(t)
	svc := NewAddressService(db)

	box := &models.BoundingBox{MinLng: -84.6, MinLat: 39.0, MaxLng: -83.9, MaxLat: 39.7}

	_, boxOnly, err := svc.SearchAddresses(models.AddressSearchParams{BBox: box, Limit: 1})
	if err != nil {
		t.Fatalf("bbox: %v", err)
	}
	rows, combined, err := svc.SearchAddresses(models.AddressSearchParams{
		BBox: box, Query: "Barendt", Limit: 500,
	})
	if err != nil {
		t.Fatalf("bbox + query: %v", err)
	}

	if combined > boxOnly {
		t.Errorf("adding a text query widened the result from %d to %d", boxOnly, combined)
	}
	for _, r := range rows {
		if r.Longitude < box.MinLng || r.Longitude > box.MaxLng {
			t.Errorf("%s escaped the box when combined with a text query", r.FullAddress)
		}
		if r.Match == nil {
			t.Error("match metadata missing when a territory filter is combined with a query")
		}
	}
	t.Logf("box alone %d, box + \"Barendt\" %d", boxOnly, combined)
}

// A box with its corners the wrong way round selects nothing, silently. It has
// to be rejected rather than returned as an empty result.
func TestBBoxValidationCatchesSwappedCorners(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"reversed longitude", "-82.9,39.9,-83.1,40.1"},
		{"reversed latitude", "-83.1,40.1,-82.9,39.9"},
		{"too few values", "-83.1,39.9,-82.9"},
		{"not a number", "-83.1,39.9,east,40.1"},
		{"latitude out of range", "-83.1,91.0,-82.9,92.0"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := models.ParseBBox(tc.raw); err == nil {
				t.Errorf("%q was accepted; a bad box returns plausible rows from the wrong place", tc.raw)
			}
		})
	}

	good := "-83.1,39.9,-82.9,40.1"
	box, err := models.ParseBBox(good)
	if err != nil {
		t.Fatalf("a valid box was rejected: %v", err)
	}
	if box.MinLng != -83.1 || box.MinLat != 39.9 || box.MaxLng != -82.9 || box.MaxLat != 40.1 {
		t.Errorf("parsed %+v; values are longitude-first", box)
	}
}

// The spatial index has to be able to serve this. Without it the endpoint is a
// sequential scan wearing a spatial API.
//
// The fixture is a few hundred rows, where a seq scan is genuinely cheaper and
// the planner is right to choose it -- so seqscan is disabled for the check.
// That answers the question that matters: can the predicate use the GIST
// index, or is its shape wrong.
func TestTerritorySearchCanUseTheSpatialIndex(t *testing.T) {
	db := setupCountTestDB(t)

	// The shared fixture does not build this; production does, in the
	// ohio_addresses migration. Without it the check would pass or fail on the
	// fixture's shape rather than on the predicate's.
	if _, err := db.Exec("CREATE INDEX IF NOT EXISTS idx_probe_geom ON ohio_addresses USING GIST (geom)"); err != nil {
		t.Fatalf("create gist index: %v", err)
	}
	if _, err := db.Exec("ANALYZE ohio_addresses"); err != nil {
		t.Fatalf("analyze: %v", err)
	}

	if _, err := db.Exec("SET enable_seqscan = off"); err != nil {
		t.Fatalf("disable seqscan: %v", err)
	}
	defer db.Exec("SET enable_seqscan = on")

	// Built from the same constants the query builder uses, so changing the
	// predicate to a non-indexable shape fails here instead of quietly passing
	// against a stale hand-copied duplicate.
	bbox := fmt.Sprintf(strings.NewReplacer("$%d", "%s").Replace(BBoxPredicateSQL),
		"-84.1", "39.0", "-84.0", "39.1")
	polygon := fmt.Sprintf(strings.NewReplacer("$%d", "%s").Replace(PolygonPredicateSQL),
		`'{"type":"Polygon","coordinates":[[[-84.1,39.0],[-84.0,39.0],[-84.0,39.1],[-84.1,39.1],[-84.1,39.0]]]}'`)

	for _, tc := range []struct {
		name  string
		where string
	}{
		{"bbox", bbox},
		{"polygon", polygon},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := db.Query("EXPLAIN SELECT id FROM ohio_addresses WHERE " + tc.where)
			if err != nil {
				t.Fatalf("explain: %v", err)
			}
			defer rows.Close()

			var plan strings.Builder
			for rows.Next() {
				var line string
				if err := rows.Scan(&line); err != nil {
					t.Fatalf("scan plan: %v", err)
				}
				plan.WriteString(line)
				plan.WriteString("\n")
			}

			if !strings.Contains(plan.String(), "Index Scan") &&
				!strings.Contains(plan.String(), "Bitmap Index Scan") {
				t.Errorf("%s predicate cannot use the GIST index:\n%s", tc.name, plan.String())
			}
			t.Logf("%s plan: %s", tc.name, strings.TrimSpace(plan.String()))
		})
	}
}

// NaN passes every range comparison, because comparisons against NaN are all
// false. Before this was caught, bbox=NaN,39.9,-82.9,40.1 parsed cleanly and
// flowed into ST_MakeEnvelope -- an empty 200 or a 500, which is the exact
// silent failure the validation exists to prevent.
func TestBBoxRejectsNonFiniteValues(t *testing.T) {
	for _, raw := range []string{
		"NaN,39.9,-82.9,40.1",
		"-83.1,nan,-82.9,40.1",
		"Inf,39.9,-82.9,40.1",
		"-83.1,39.9,-Inf,40.1",
	} {
		if box, err := models.ParseBBox(raw); err == nil {
			t.Errorf("%q was accepted as %+v; it slips past every range check", raw, box)
		}
	}
}

// Exact containment matters for anyone tiling a territory into adjacent boxes:
// the && operator compares float4 bounding boxes rounded outward, so it
// returns points up to ~0.7m outside the rectangle and every address near a
// shared edge lands in both tiles.
func TestAdjacentBoxesDoNotDoubleCount(t *testing.T) {
	db := setupCountTestDB(t)
	svc := NewAddressService(db)

	// A point placed a hair outside the western box's eastern edge.
	if _, err := db.Exec(`
		INSERT INTO ohio_addresses (hash, house_number, street, unit, city, district, region, postcode, county, geom, full_address)
		VALUES ('edge-point', '9', 'Edge Road', '', 'Columbus', 'FRA', 'OH', '43004', 'Franklin',
		        ST_SetSRID(ST_MakePoint(-83.0999995, 39.5), 4326), '9 Edge Road, Columbus, OH 43004')
	`); err != nil {
		t.Fatalf("seed edge point: %v", err)
	}

	west := &models.BoundingBox{MinLng: -83.2, MinLat: 39.4, MaxLng: -83.1, MaxLat: 39.6}
	east := &models.BoundingBox{MinLng: -83.1, MinLat: 39.4, MaxLng: -83.0, MaxLat: 39.6}

	count := func(box *models.BoundingBox) int {
		rows, _, err := svc.SearchAddresses(models.AddressSearchParams{BBox: box, Limit: 500})
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		n := 0
		for _, r := range rows {
			if r.Hash == "edge-point" {
				n++
			}
		}
		return n
	}

	inWest, inEast := count(west), count(east)
	t.Logf("edge point appears in west=%d east=%d", inWest, inEast)
	if inWest+inEast != 1 {
		t.Errorf("the point on the shared edge appears %d times across two adjacent boxes, want exactly 1",
			inWest+inEast)
	}
}
