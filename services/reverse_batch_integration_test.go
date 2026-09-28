package services

import (
	"database/sql"
	"fmt"
	"testing"
)

// The batch answers each coordinate exactly as /reverse does, and answers them
// all in a handful of queries rather than a handful per item.
func TestReverseBatchMatchesSingleReverse(t *testing.T) {
	db := setupReverseDB(t)

	// The same points the single-reverse tests use: one beside an address, one
	// far from any, and one outside the fixture's state.
	points := []ReverseBatchItem{
		{ID: "downtown", Lat: 39.9613, Lng: -83.0008},
		{ID: "far", Lat: 41.7300, Lng: -83.6000},
		{ID: "elsewhere", Lat: 45.0000, Lng: -100.0000},
	}

	batch, err := ReverseGeocodeBatch(db, points, 0)
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	if batch.Total != 3 || batch.BillableUnits != 3 {
		t.Errorf("total %d, billable %d; want 3 and 3", batch.Total, batch.BillableUnits)
	}

	for i, item := range points {
		single, err := ReverseGeocode(db, item.Lat, item.Lng, 0)
		if err != nil {
			t.Fatalf("single %s: %v", item.ID, err)
		}
		got := batch.Results[i]
		if got.ID != item.ID {
			t.Errorf("result %d carries id %q, want %q", i, got.ID, item.ID)
		}
		if got.ReverseResult == nil {
			t.Fatalf("%s has no result: %s", item.ID, got.Error)
		}
		if fmt.Sprint(addressOf(got.ReverseResult)) != fmt.Sprint(addressOf(single)) {
			t.Errorf("%s address = %v, single reverse said %v", item.ID, addressOf(got.ReverseResult), addressOf(single))
		}
		if deref(got.County) != deref(single.County) {
			t.Errorf("%s county = %s, single reverse said %s", item.ID, deref(got.County), deref(single.County))
		}
		if fmt.Sprint(stateOf(got.ReverseResult)) != fmt.Sprint(stateOf(single)) {
			t.Errorf("%s state = %s, single reverse said %s", item.ID, stateOf(got.ReverseResult), stateOf(single))
		}
		if fmt.Sprint(zipOf(got.ReverseResult)) != fmt.Sprint(zipOf(single)) {
			t.Errorf("%s zip = %v, single reverse said %v", item.ID, zipOf(got.ReverseResult), zipOf(single))
		}
		if deref(got.Timezone) != deref(single.Timezone) || got.TimezoneSource != single.TimezoneSource {
			t.Errorf("%s timezone = %s/%s, single reverse said %s/%s",
				item.ID, deref(got.Timezone), got.TimezoneSource, deref(single.Timezone), single.TimezoneSource)
		}
	}
}

func addressOf(r *ReverseResult) string {
	if r == nil || r.Address == nil {
		return "<none>"
	}
	return fmt.Sprintf("%s @%.1fm acc=%s tz=%s", r.Address.FullAddress, r.Address.DistanceMeters,
		r.Address.Accuracy, deref(r.Address.Timezone))
}

// deref renders a *string by value: comparing the pointers compares addresses,
// which are never equal between two queries and so compare nothing.
func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

func stateOf(r *ReverseResult) string {
	if r == nil || r.State == nil {
		return "<none>"
	}
	return r.State.Code + " " + r.State.Name
}

func zipOf(r *ReverseResult) string {
	if r == nil || r.Zip == nil {
		return "<none>"
	}
	return fmt.Sprintf("%s %s acc=%s", r.Zip.ZipCode, r.Zip.CityName, r.Zip.Accuracy)
}

// One bad coordinate fails its own item. Throwing away the other answers
// because item 40 had a typo is the thing a batch endpoint exists to avoid.
func TestReverseBatchRejectsOnlyTheBadItems(t *testing.T) {
	db := setupReverseDB(t)

	batch, err := ReverseGeocodeBatch(db, []ReverseBatchItem{
		{ID: "good", Lat: 39.9613, Lng: -83.0008},
		{ID: "lat", Lat: 91, Lng: -83},
		{ID: "lng", Lat: 39.96, Lng: -181},
		{ID: "also good", Lat: 39.9615, Lng: -83.0020},
	}, 0)
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	if batch.Results[0].Error != "" || batch.Results[3].Error != "" {
		t.Errorf("a good item carried an error: %q, %q", batch.Results[0].Error, batch.Results[3].Error)
	}
	if batch.Results[0].ReverseResult == nil || batch.Results[0].Address == nil {
		t.Error("the first good item did not resolve")
	}
	for _, i := range []int{1, 2} {
		if batch.Results[i].Error == "" {
			t.Errorf("item %d was accepted with an impossible coordinate", i)
		}
		if batch.Results[i].Found {
			t.Errorf("item %d is marked found", i)
		}
		if batch.Results[i].ReverseResult != nil {
			t.Errorf("item %d carries a result", i)
		}
	}
	// Billed for what was asked, including the refused items: the check is the
	// work. Same as the forward batch.
	if batch.BillableUnits != 4 {
		t.Errorf("billable units = %d, want 4", batch.BillableUnits)
	}
	if batch.Found != 2 {
		t.Errorf("found = %d, want 2", batch.Found)
	}
}

// A point in the ocean resolves to nothing, which is an answer rather than a
// hit -- and a caller counting hits should not have to compare five fields.
func TestReverseBatchMarksEmptyAnswersUnfound(t *testing.T) {
	db := setupReverseDB(t)

	batch, err := ReverseGeocodeBatch(db, []ReverseBatchItem{
		{ID: "atlantic", Lat: 30.0, Lng: -40.0},
		{ID: "columbus", Lat: 39.9613, Lng: -83.0008},
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if batch.Results[0].Found {
		t.Error("a point in the Atlantic is marked found")
	}
	if batch.Results[0].Error != "" {
		t.Errorf("a valid coordinate with no data carried an error: %q", batch.Results[0].Error)
	}
	if !batch.Results[1].Found {
		t.Error("a point beside an address is not marked found")
	}
	if batch.Found != 1 {
		t.Errorf("found = %d, want 1", batch.Found)
	}
}

func TestReverseBatchLimits(t *testing.T) {
	db := setupReverseDB(t)

	if _, err := ReverseGeocodeBatch(db, nil, 0); err == nil {
		t.Error("an empty batch was accepted")
	}
	oversized := make([]ReverseBatchItem, MaxBatchItems+1)
	if _, err := ReverseGeocodeBatch(db, oversized, 0); err == nil {
		t.Errorf("a batch of %d was accepted", len(oversized))
	}

	// The radius is bounded like the single endpoint's, and reported.
	batch, err := ReverseGeocodeBatch(db, []ReverseBatchItem{{Lat: 39.9613, Lng: -83.0008}}, 10_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if batch.SearchRadiusMeters != maxReverseRadiusMeters {
		t.Errorf("radius = %g, want it capped at %g", batch.SearchRadiusMeters, maxReverseRadiusMeters)
	}
	if batch.Results[0].SearchRadiusMeters != maxReverseRadiusMeters {
		t.Errorf("the item reports radius %g", batch.Results[0].SearchRadiusMeters)
	}

	// A radius small enough to exclude the address next door, so the bound is
	// doing something rather than being echoed back.
	tight, err := ReverseGeocodeBatch(db, []ReverseBatchItem{{Lat: 41.7300, Lng: -83.6000}}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if tight.Results[0].Address != nil {
		t.Errorf("found %s within 100m", tight.Results[0].Address.FullAddress)
	}
}

// countingQuerier counts the statements a call makes.
type countingQuerier struct {
	inner   querier
	queries int
}

func (c *countingQuerier) Query(query string, args ...interface{}) (*sql.Rows, error) {
	c.queries++
	return c.inner.Query(query, args...)
}

func (c *countingQuerier) QueryRow(query string, args ...interface{}) *sql.Row {
	c.queries++
	return c.inner.QueryRow(query, args...)
}

// The point of a batch endpoint is the round trips it saves. Answering each
// item the way /reverse does would be five queries per item, so this counts
// them to keep per-item resolution from creeping back in.
func TestReverseBatchQueryCountDoesNotGrowWithItems(t *testing.T) {
	db := setupReverseDB(t)

	count := func(t *testing.T, items int) int {
		t.Helper()
		counter := &countingQuerier{inner: db}
		batch := make([]ReverseBatchItem, items)
		for i := range batch {
			batch[i] = ReverseBatchItem{Lat: 39.9612 + float64(i)*0.0001, Lng: -83.0007}
		}
		if _, err := ReverseGeocodeBatch(counter, batch, 0); err != nil {
			t.Fatal(err)
		}
		return counter.queries
	}

	one := count(t, 1)
	fifty := count(t, 50)
	t.Logf("queries: 1 item = %d, 50 items = %d", one, fifty)

	// Fifty items must not cost fifty times one. The batch runs a fixed set of
	// queries over all the points, plus one timezone fallback per point the
	// zone polygons do not cover -- so it may grow, but nothing like per-item
	// resolution would.
	if fifty > one*4 {
		t.Errorf("50 items took %d queries against %d for one: the work is scaling per item", fifty, one)
	}
	// And the fixed part is genuinely fixed: one item costs a handful, not the
	// five-plus a per-item implementation would.
	if one > 8 {
		t.Errorf("a single item took %d queries", one)
	}
}

// Each lookup degrades on its own, as /reverse's fields do. A deployment part
// way through its migrations must still get the answers it can rather than
// losing all hundred to one lookup that cannot run yet.
//
// Simulated by dropping the column each query depends on, which is what a
// pending migration looks like -- and which stays inside the private schema.
// Dropping the table instead would expose the real one in public: this test
// passed against an empty database and answered "Franklin" from production
// data against a populated one.
func TestReverseBatchDegradesPerLookup(t *testing.T) {
	for _, tc := range []struct {
		table, column string
	}{
		{"ohio_counties", "bounds_geometry"},
		{"us_states", "geometry"},
		{"zip_codes", "geog"},
		{"ohio_addresses", "geom"},
	} {
		t.Run("without "+tc.table+"."+tc.column, func(t *testing.T) {
			db := setupReverseDB(t)
			if _, err := db.Exec(fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s CASCADE", tc.table, tc.column)); err != nil {
				t.Fatalf("drop %s.%s: %v", tc.table, tc.column, err)
			}

			batch, err := ReverseGeocodeBatch(db, []ReverseBatchItem{
				{ID: "a", Lat: 39.9613, Lng: -83.0008},
				{ID: "b", Lat: 41.5045, Lng: -81.6931},
			}, 0)
			if err != nil {
				t.Fatalf("the whole batch failed for want of %s.%s: %v", tc.table, tc.column, err)
			}
			if len(batch.Results) != 2 {
				t.Fatalf("got %d results", len(batch.Results))
			}
			for _, r := range batch.Results {
				if r.Error != "" {
					t.Errorf("%s carried an error: %s", r.ID, r.Error)
				}
				if r.ReverseResult == nil {
					t.Fatalf("%s has no result", r.ID)
				}
			}

			// The field backed by the missing column is empty; the others are
			// not the caller's problem.
			first := batch.Results[0]
			switch tc.table {
			case "ohio_counties":
				if first.County != nil {
					t.Errorf("county answered without its geometry: %v", *first.County)
				}
				if first.Address == nil {
					t.Error("addresses stopped answering because counties could not")
				}
			case "us_states":
				if first.State != nil {
					t.Errorf("state answered without its geometry: %+v", first.State)
				}
				if first.Address == nil {
					t.Error("addresses stopped answering because states could not")
				}
			case "zip_codes":
				if first.Zip != nil {
					t.Errorf("zip answered without geog: %+v", first.Zip)
				}
				if first.County == nil {
					t.Error("counties stopped answering because ZIPs could not")
				}
			case "ohio_addresses":
				if first.Address != nil {
					t.Errorf("address answered without geom: %+v", first.Address)
				}
				if first.County == nil {
					t.Error("counties stopped answering because addresses could not")
				}
			}
		})
	}
}

// Two items at the same coordinate must get the same answer, and each must get
// its own -- the queries return one row per point, and a mapping that keyed by
// coordinate rather than by position would collapse them.
func TestReverseBatchHandlesDuplicateCoordinates(t *testing.T) {
	db := setupReverseDB(t)

	batch, err := ReverseGeocodeBatch(db, []ReverseBatchItem{
		{ID: "first", Lat: 39.9613, Lng: -83.0008},
		{ID: "second", Lat: 39.9613, Lng: -83.0008},
		{ID: "third", Lat: 41.5045, Lng: -81.6931},
		{ID: "fourth", Lat: 39.9613, Lng: -83.0008},
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Results) != 4 {
		t.Fatalf("got %d results for 4 items", len(batch.Results))
	}
	for _, i := range []int{0, 1, 3} {
		r := batch.Results[i]
		if r.ReverseResult == nil || r.Address == nil {
			t.Fatalf("result %d (%s) did not resolve", i, r.ID)
		}
		if r.Address.FullAddress != batch.Results[0].Address.FullAddress {
			t.Errorf("%s resolved to %s, but the identical coordinate at 0 gave %s",
				r.ID, r.Address.FullAddress, batch.Results[0].Address.FullAddress)
		}
	}
	// And the odd one out kept its own answer rather than a neighbour's.
	if batch.Results[2].ReverseResult == nil {
		t.Fatal("the third item did not resolve")
	}
	if batch.Results[2].Address != nil &&
		batch.Results[2].Address.FullAddress == batch.Results[0].Address.FullAddress {
		t.Error("a different coordinate got the first item's answer")
	}
	// Every item is billed, duplicates included.
	if batch.BillableUnits != 4 {
		t.Errorf("billable units = %d, want 4", batch.BillableUnits)
	}
}

// found means the coordinate resolved to a place, judged against the radius
// the caller asked for -- not against the cap. A point 45km off the coast with
// a one-metre radius is not a hit because a ZIP centroid happens to be 45km
// away.
func TestReverseBatchFoundRespectsTheRequestedRadius(t *testing.T) {
	db := setupReverseDB(t)

	// Just north of the fixture's state envelope (which ends at 42.0), so
	// nothing contains the point and only the ZIP lookup has anything to say
	// -- and the nearest ZIP is ~43km off, inside the 50km cap, so a wider
	// radius can reach it.
	far := []ReverseBatchItem{{ID: "offshore", Lat: 42.05, Lng: -83.6}}

	tight, err := ReverseGeocodeBatch(db, far, 1)
	if err != nil {
		t.Fatal(err)
	}
	if tight.Results[0].Zip == nil {
		t.Skip("fixture has no ZIP to be far from")
	}
	distance := tight.Results[0].Zip.DistanceMeters
	if distance <= 1 {
		t.Skipf("the nearest ZIP is %.0fm away, too close to test the bound", distance)
	}
	if tight.Results[0].Found {
		t.Errorf("found with a 1m radius and the nearest ZIP %.0fm away", distance)
	}

	if distance >= maxReverseRadiusMeters {
		t.Skipf("the nearest ZIP is %.0fm away, beyond the %.0fm cap, so no radius reaches it",
			distance, maxReverseRadiusMeters)
	}

	// The same point with a radius that reaches it.
	loose, err := ReverseGeocodeBatch(db, far, distance+1000)
	if err != nil {
		t.Fatal(err)
	}
	if !loose.Results[0].Found {
		t.Errorf("not found with a radius of %.0fm and the ZIP %.0fm away", distance+1000, distance)
	}
}
