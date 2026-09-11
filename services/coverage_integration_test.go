package services

import (
	"testing"
	"time"
)

// The endpoint exists so a caller can find out that, say, Michigan has no
// street data -- instead of querying it, getting nothing, and filing a ticket.
func TestCoverageReportsWhatIsLoaded(t *testing.T) {
	db := setupCountTestDB(t)
	ResetCoverageCache()

	snapshot, err := GetCoverage(db)
	if err != nil {
		t.Fatalf("coverage: %v", err)
	}
	if len(snapshot.States) == 0 {
		t.Fatal("no states reported for a seeded table")
	}

	var total int
	for _, s := range snapshot.States {
		if s.State == "" {
			t.Error("a state row came back with no state code")
		}
		if s.Counties <= 0 {
			t.Errorf("state %s reports %d counties", s.State, s.Counties)
		}
		total += s.Addresses
	}
	if total != snapshot.TotalRows {
		t.Errorf("per-state addresses sum to %d, total says %d", total, snapshot.TotalRows)
	}
	t.Logf("coverage: %d state(s), %d addresses", len(snapshot.States), snapshot.TotalRows)
}

// The alternative to caching is a GROUP BY over every address row on every
// request, which is the mistake the address search count query was making.
func TestCoverageIsCachedAndInvalidatedOnImport(t *testing.T) {
	db := setupCountTestDB(t)
	ResetCoverageCache()

	first, err := GetCoverage(db)
	if err != nil {
		t.Fatalf("first: %v", err)
	}

	second, err := GetCoverage(db)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if !first.GeneratedAt.Equal(second.GeneratedAt) {
		t.Error("second call recomputed instead of serving the cached snapshot")
	}

	// An import is the only thing that changes coverage.
	time.Sleep(2 * time.Millisecond)
	ResetCoverageCache()
	third, err := GetCoverage(db)
	if err != nil {
		t.Fatalf("third: %v", err)
	}
	if !third.GeneratedAt.After(second.GeneratedAt) {
		t.Error("snapshot was not rebuilt after the cache was invalidated")
	}
}

// The drill-down has to agree with the summary, or the two views contradict
// each other and neither can be trusted.
func TestStateCoverageAgreesWithTheSummary(t *testing.T) {
	db := setupCountTestDB(t)
	ResetCoverageCache()

	snapshot, err := GetCoverage(db)
	if err != nil {
		t.Fatalf("coverage: %v", err)
	}

	for _, s := range snapshot.States {
		counties, err := GetStateCoverage(db, s.State)
		if err != nil {
			t.Fatalf("state coverage for %s: %v", s.State, err)
		}
		if len(counties) != s.Counties {
			t.Errorf("state %s: summary says %d counties, drill-down returns %d",
				s.State, s.Counties, len(counties))
		}

		var sum int
		for _, c := range counties {
			sum += c.Addresses
		}
		if sum != s.Addresses {
			t.Errorf("state %s: counties sum to %d, summary says %d", s.State, sum, s.Addresses)
		}
	}
}

// An unknown state is an empty list, not an error -- that is the answer the
// caller came for.
func TestUnknownStateReturnsEmptyNotError(t *testing.T) {
	db := setupCountTestDB(t)

	counties, err := GetStateCoverage(db, "MI")
	if err != nil {
		t.Fatalf("unknown state should not error: %v", err)
	}
	if len(counties) != 0 {
		t.Errorf("got %d counties for an unloaded state", len(counties))
	}
}
