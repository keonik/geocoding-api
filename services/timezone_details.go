package services

import (
	"strconv"
	"sync"
	"time"

	// Imported here rather than only in main: without it these lookups read
	// the host's zoneinfo, so the details would be empty on an image or a
	// test runner that ships none, and the tests would be attesting to the
	// machine they ran on.
	_ "time/tzdata"

	"geocoding-api/models"
)

// detailsCache holds what has already been worked out, keyed by zone and
// year. time.LoadLocation parses the zone file on every call, and a page of
// addresses asks about the same handful of zones over and over; the answers
// only change when the year does.
var detailsCache sync.Map

// timezoneDetails describes a zone as of the year containing at.
//
// Zones change: Chile has rewritten its rules repeatedly, and several have
// abandoned DST outright. Reading the answer out of the tz database for a
// stated year is the only way to be right about the one being asked about.
// Nil when the name is not in the database -- a deployment without tzdata, or
// a zone newer than the binary -- which leaves the zone name to stand alone
// rather than inventing an offset for it.
func timezoneDetails(name string, at time.Time) *models.TimezoneDetails {
	key := name + "@" + strconv.Itoa(at.Year())
	if cached, ok := detailsCache.Load(key); ok {
		return cached.(*models.TimezoneDetails)
	}
	details := computeTimezoneDetails(name, at)
	detailsCache.Store(key, details)
	return details
}

// computeTimezoneDetails is timezoneDetails without the cache.
func computeTimezoneDetails(name string, at time.Time) *models.TimezoneDetails {
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil
	}

	// January and July catch the shift in either hemisphere. Standard time is
	// the smaller offset of the two: DST only ever moves clocks forward, and
	// that holds even where the tz database models it the other way round --
	// Europe/Dublin calls summer standard and winter negative DST, and this
	// still reports GMT, observing DST, which is what a caller means.
	//
	// Two samples can miss a shift that happens outside them: Africa/Casablanca
	// steps back for Ramadan, on a lunar date, and reads here as observing no
	// DST. The zones this API serves are US ones, where January and July
	// straddle every transition.
	jan := time.Date(at.Year(), time.January, 15, 12, 0, 0, 0, loc)
	jul := time.Date(at.Year(), time.July, 15, 12, 0, 0, 0, loc)
	janName, janOffset := jan.Zone()
	julName, julOffset := jul.Zone()

	standardName, standardOffset := janName, janOffset
	if julOffset < janOffset {
		standardName, standardOffset = julName, julOffset
	}

	return &models.TimezoneDetails{
		UTCOffset:    float64(standardOffset) / 3600,
		ObservesDST:  janOffset != julOffset,
		Abbreviation: standardName,
	}
}
