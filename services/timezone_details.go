package services

import "time"

// TimezoneDetails is what a caller needs to do arithmetic with a zone without
// shipping a copy of the IANA database.
//
// The name alone is the honest identifier, and the one thing that stays true
// as rules change -- but a caller formatting a timestamp or labelling a column
// needs the offset and the abbreviation, and Geocodio returns them, so an
// integration moving across should not have to look them up.
type TimezoneDetails struct {
	// UTCOffset is the standard-time offset in hours, so -5 for US Eastern.
	// Not the offset in force right now: that changes twice a year, and a
	// value that means something different depending on when it was fetched
	// is a poor thing to store. Fractional for the zones that need it --
	// Pacific/Marquesas is -9.5.
	UTCOffset float64 `json:"utc_offset"`

	// ObservesDST is whether the zone shifts at all during the year. It says
	// nothing about whether DST is in force at this moment.
	ObservesDST bool `json:"observes_dst"`

	// Abbreviation is the standard-time abbreviation, "EST" for US Eastern.
	// Some zones have no abbreviation of their own and the IANA database
	// gives a numeric one such as "+0530", which is passed through as it is.
	Abbreviation string `json:"abbreviation"`
}

// timezoneDetails describes a zone as of the year containing at.
//
// Zones change: Chile has rewritten its rules repeatedly, and several have
// abandoned DST outright. Reading the answer out of the tz database for a
// stated year is the only way to be right about the one being asked about.
// Nil when the name is not in the database -- a deployment without tzdata, or
// a zone newer than the binary -- which leaves the zone name to stand alone
// rather than inventing an offset for it.
func timezoneDetails(name string, at time.Time) *TimezoneDetails {
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil
	}

	// January and July catch the shift in either hemisphere. Standard time is
	// the smaller offset of the two: DST only ever moves clocks forward.
	jan := time.Date(at.Year(), time.January, 15, 12, 0, 0, 0, loc)
	jul := time.Date(at.Year(), time.July, 15, 12, 0, 0, 0, loc)
	janName, janOffset := jan.Zone()
	julName, julOffset := jul.Zone()

	standardName, standardOffset := janName, janOffset
	if julOffset < janOffset {
		standardName, standardOffset = julName, julOffset
	}

	return &TimezoneDetails{
		UTCOffset:    float64(standardOffset) / 3600,
		ObservesDST:  janOffset != julOffset,
		Abbreviation: standardName,
	}
}
