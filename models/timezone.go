package models

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
