package services

import (
	"testing"
	"time"
)

// The offsets and abbreviations a caller would otherwise need the IANA
// database to work out. Asked as of 2026, since the answers are only true of
// a stated year.
func TestTimezoneDetails(t *testing.T) {
	at := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		zone         string
		offset       float64
		dst          bool
		abbreviation string
	}{
		{"America/New_York", -5, true, "EST"},
		{"America/Chicago", -6, true, "CST"},
		{"America/Denver", -7, true, "MST"},
		// Arizona keeps standard time all year, which is the case an offset
		// alone cannot express.
		{"America/Phoenix", -7, false, "MST"},
		{"America/Los_Angeles", -8, true, "PST"},
		{"America/Anchorage", -9, true, "AKST"},
		{"Pacific/Honolulu", -10, false, "HST"},
		{"America/Indiana/Indianapolis", -5, true, "EST"},
		{"America/Indiana/Tell_City", -6, true, "CST"},
		{"America/Puerto_Rico", -4, false, "AST"},
		{"Pacific/Guam", 10, false, "ChST"},
		// Southern hemisphere: the shift runs the other way round the year,
		// so taking the smaller offset as standard has to hold there too.
		{"Australia/Sydney", 10, true, "AEST"},
		// A half-hour zone, which is why the offset is not an integer.
		{"Asia/Kolkata", 5.5, false, "IST"},
	}
	for _, c := range cases {
		t.Run(c.zone, func(t *testing.T) {
			d := timezoneDetails(c.zone, at)
			if d == nil {
				t.Fatal("no details; is tzdata missing?")
			}
			if d.UTCOffset != c.offset {
				t.Errorf("utc_offset = %g, want %g", d.UTCOffset, c.offset)
			}
			if d.ObservesDST != c.dst {
				t.Errorf("observes_dst = %v, want %v", d.ObservesDST, c.dst)
			}
			if d.Abbreviation != c.abbreviation {
				t.Errorf("abbreviation = %q, want %q", d.Abbreviation, c.abbreviation)
			}
		})
	}
}

// An unknown zone leaves the name to stand alone rather than inventing an
// offset for it.
func TestTimezoneDetailsUnknownZone(t *testing.T) {
	if d := timezoneDetails("America/Nowhere", time.Now()); d != nil {
		t.Errorf("invented details for an unknown zone: %+v", d)
	}
}

// The details describe the year asked about, not the year the binary was
// built: Chile moved its DST dates, and zones abandon DST outright.
func TestTimezoneDetailsFollowTheYear(t *testing.T) {
	// Until 2022 Mexico City observed DST; from 2023 it does not.
	before := timezoneDetails("America/Mexico_City", time.Date(2020, time.June, 1, 0, 0, 0, 0, time.UTC))
	after := timezoneDetails("America/Mexico_City", time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC))
	if before == nil || after == nil {
		t.Fatal("no details")
	}
	if !before.ObservesDST {
		t.Error("2020 Mexico City should observe DST")
	}
	if after.ObservesDST {
		t.Error("2026 Mexico City should not observe DST")
	}
}

// The cache is keyed by year as well as zone, so an answer for one year
// cannot be served for another.
func TestTimezoneDetailsCacheIsKeyedByYear(t *testing.T) {
	zone := "America/Mexico_City"
	before := timezoneDetails(zone, time.Date(2020, time.June, 1, 0, 0, 0, 0, time.UTC))
	after := timezoneDetails(zone, time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC))
	again := timezoneDetails(zone, time.Date(2020, time.September, 9, 0, 0, 0, 0, time.UTC))
	if before == nil || after == nil || again == nil {
		t.Fatal("no details")
	}
	if before.ObservesDST == after.ObservesDST {
		t.Error("both years answered the same; the cache is ignoring the year")
	}
	if again.ObservesDST != before.ObservesDST {
		t.Error("a second look at the same year disagreed with the first")
	}
}

// An unknown zone is cached as "no details" rather than parsed again on
// every row of every page.
func TestTimezoneDetailsCachesMisses(t *testing.T) {
	at := time.Now()
	if d := timezoneDetails("Mars/Olympus_Mons", at); d != nil {
		t.Fatalf("invented details: %+v", d)
	}
	if d := timezoneDetails("Mars/Olympus_Mons", at); d != nil {
		t.Fatalf("invented details on the second call: %+v", d)
	}
}
