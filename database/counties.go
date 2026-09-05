package database

import (
	"fmt"
	"log"
)

// LoadOhioCountyBoundaries loads county boundary polygons from the
// oh/*-addresses-county.geojson.meta sidecars and reports how many counties
// the table holds afterwards.
//
// Exported so an admin endpoint can run it on demand, which matters because
// the data arrives out of band: if a download fails, the fallback writes
// placeholder meta files with no "bounds" member, the loader skips every one
// of them, and nothing is inserted. That is the safe outcome -- placeholders
// cannot pollute the table -- but it means a successful-looking run can still
// load nothing.
func LoadOhioCountyBoundaries() (int, error) {
	if err := loadOhioCountyBoundaries(); err != nil {
		return 0, err
	}
	return CountOhioCounties()
}

// CountOhioCounties returns the number of county boundaries currently stored.
func CountOhioCounties() (int, error) {
	var count int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM ohio_counties`).Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to count county boundaries: %w", err)
	}
	return count, nil
}

// EnsureCountyBoundaries loads county boundaries at boot when the table is
// empty.
//
// Deliberately not a migration. Migration 9 already does this, and that is the
// problem: it loaded zero counties, returned nil, and was recorded as applied,
// so it can never run again no matter how many times the service is
// redeployed or how the source data changes. A boot-time check is idempotent
// by outcome rather than by version -- it retries on every start until the
// table actually has rows, and costs one COUNT(*) once it does.
func EnsureCountyBoundaries() error {
	count, err := CountOhioCounties()
	if err != nil {
		return err
	}
	if count > 0 {
		log.Printf("County boundaries already loaded (%d counties)", count)
		return nil
	}

	log.Println("ohio_counties is empty; attempting to load county boundaries")
	loaded, err := LoadOhioCountyBoundaries()
	if err != nil {
		return err
	}
	if loaded == 0 {
		log.Println("No county boundaries loaded: no oh/*-addresses-county.geojson.meta " +
			"files carried a bounds polygon. Will retry on next start.")
	} else {
		log.Printf("Loaded %d county boundaries", loaded)
	}
	return nil
}
