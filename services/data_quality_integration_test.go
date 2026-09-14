package services

import (
	"testing"

	"geocoding-api/models"
)

// Every problem below is silent: a search still returns rows, an import still
// reports success, nothing errors. Production carried eleven distinct region
// codes and 985,634 stateless addresses, and the only reason anyone found out
// was an unrelated endpoint happening to group by region.
func TestDataQualityFindsTheProblemsProductionHad(t *testing.T) {
	db := setupCountTestDB(t)
	ResetDataQualityCache()

	// The exact shapes production held.
	if _, err := db.Exec(`
		INSERT INTO ohio_addresses (hash, house_number, street, unit, city, district, region, postcode, county, geom, full_address)
		VALUES
		  ('dq-ontario','1','Main','','Toronto','','ON','','York',
		   ST_SetSRID(ST_MakePoint(-79.4,43.7),4326),'1 Main, Toronto'),
		  ('dq-truncated','2','Main','','Somewhere','','BE','','Unknown',
		   ST_SetSRID(ST_MakePoint(-83.0,40.0),4326),'2 Main'),
		  ('dq-single','3','Main','','Somewhere','','O','','Unknown',
		   ST_SetSRID(ST_MakePoint(-83.0,40.0),4326),'3 Main'),
		  ('dq-lowercase','4','Main','','Columbus','','oh','43004','Franklin',
		   ST_SetSRID(ST_MakePoint(-83.0,40.0),4326),'4 Main'),
		  ('dq-nocounty','5','Main','','Columbus','','OH','43004','',
		   ST_SetSRID(ST_MakePoint(-83.0,40.0),4326),'5 Main'),
		  ('dq-nozip','6','Main','','Columbus','','OH','','Franklin',
		   ST_SetSRID(ST_MakePoint(-83.0,40.0),4326),'6 Main'),
		  ('dq-atsea','7','Main','','Nowhere','','OH','43004','Franklin',
		   ST_SetSRID(ST_MakePoint(-40.0,35.0),4326),'7 Main')
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	report, err := GetDataQuality(db)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	byRegion := map[string]RegionIssue{}
	for _, r := range report.InvalidRegions {
		byRegion[r.Region] = r
	}
	for _, want := range []string{"ON", "BE", "O", "oh"} {
		if _, ok := byRegion[want]; !ok {
			t.Errorf("region %q was not reported as invalid; it is its own uniqueness bucket", want)
		}
	}
	// A valid code must not be flagged.
	if _, flagged := byRegion["OH"]; flagged {
		t.Error("OH was reported as invalid")
	}
	// The reason has to say what is wrong, or the report is just a list.
	if r, ok := byRegion["oh"]; ok && r.Reason == "" {
		t.Error("the lowercase region carries no explanation")
	}

	if report.BlankCounty < 1 {
		t.Error("a row with no county was not counted")
	}
	if report.BlankPostcode < 1 {
		t.Error("a row with no postcode was not counted")
	}
	if report.OutsideUS < 1 {
		t.Error("a row in the mid-Atlantic was not counted as outside US bounds")
	}
	if report.TotalAddresses < 7 {
		t.Errorf("total = %d, want at least the 7 seeded rows", report.TotalAddresses)
	}

	t.Logf("invalid regions: %d, blank county: %d, blank postcode: %d, outside US: %d",
		len(report.InvalidRegions), report.BlankCounty, report.BlankPostcode, report.OutsideUS)
}

// The scan is aggregates over the whole table, so it is cached -- the same
// reasoning as coverage, and the same mistake the address search count query
// was making before it was fixed.
func TestDataQualityIsCached(t *testing.T) {
	db := setupCountTestDB(t)
	ResetDataQualityCache()

	first, err := GetDataQuality(db)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := GetDataQuality(db)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if !first.GeneratedAt.Equal(second.GeneratedAt) {
		t.Error("the second call rescanned instead of serving the cached report")
	}
}

// Coverage folded every blank region into the Ohio bucket, which is why nobody
// saw 985,634 stateless rows and why the Ohio count read about a million higher
// than it was.
func TestCoverageReportsBlankRegionsSeparately(t *testing.T) {
	db := setupCountTestDB(t)
	ResetCoverageCache()

	if _, err := db.Exec(`
		INSERT INTO ohio_addresses (hash, house_number, street, unit, city, district, region, postcode, county, geom, full_address)
		VALUES ('cov-blank','1','Main','','Columbus','','','43004','Franklin',
		        ST_SetSRID(ST_MakePoint(-83.0,40.0),4326),'1 Main')
	`); err != nil {
		t.Fatalf("seed blank region: %v", err)
	}

	snapshot, err := GetCoverage(db)
	if err != nil {
		t.Fatalf("coverage: %v", err)
	}

	var blank, ohio int
	for _, s := range snapshot.States {
		switch s.State {
		case "(none)":
			blank = s.Addresses
		case "OH":
			ohio = s.Addresses
		}
	}

	if blank != 1 {
		t.Errorf("blank regions reported as %d rows under '(none)', want 1 -- they are being hidden in another bucket", blank)
	}
	if ohio == 0 {
		t.Error("no Ohio rows reported at all")
	}
	t.Logf("coverage: OH %d, (none) %d", ohio, blank)
}

// The drill-down must not inherit the same fold, or it disagrees with the
// summary it sits under.
func TestStateCoverageDoesNotAbsorbBlankRegions(t *testing.T) {
	db := setupCountTestDB(t)
	ResetCoverageCache()

	if _, err := db.Exec(`
		INSERT INTO ohio_addresses (hash, house_number, street, unit, city, district, region, postcode, county, geom, full_address)
		VALUES ('cov-blank2','2','Main','','Columbus','','','43004','Blankshire',
		        ST_SetSRID(ST_MakePoint(-83.0,40.0),4326),'2 Main')
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	counties, err := GetStateCoverage(db, "OH")
	if err != nil {
		t.Fatalf("state coverage: %v", err)
	}
	for _, c := range counties {
		if c.County == "Blankshire" {
			t.Error("a blank-region row was counted under OH in the drill-down")
		}
	}
}

var _ = models.AddressSearchParams{}
