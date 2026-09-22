package services

import (
	"database/sql"
	"fmt"
	"time"

	"geocoding-api/models"

	"github.com/lib/pq"
)

// timezoneSearchMeters bounds a nearest-ZIP search that has no state to hold
// it: a reverse lookup on a point in no state -- open water, mostly -- where a
// distant centroid is no evidence and would hand a point mid-lake the zone of
// whichever shore is nearest; and the per-address fallback, which runs once
// per row and so has to stay cheap.
//
// The bound has to be a plain ST_DWithin predicate for GIST to use it. Folded
// into a CASE it became a filter, and a point in the Atlantic walked every ZIP
// in the index: 556ms against 0.5ms, measured over 40,000 ZIPs.
const timezoneSearchMeters = 50000.0

// nearestZipTimezoneSQL finds the zone of the nearest ZIP centroid within $3
// metres of the point ($1 longitude, $2 latitude), in any state.
const nearestZipTimezoneSQL = `
	SELECT z.timezone FROM zip_codes z
	WHERE z.timezone <> ''
	  AND ST_DWithin(z.geog, ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography, $3, false)
	ORDER BY z.geog <-> ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography
	LIMIT 1`

// nearestZipInStateTimezoneSQL finds the zone of the nearest ZIP centroid in
// state $3.
//
// Restricted to the state because many zone lines are state lines: just inside
// Indiana, across from Illinois, the nearest centroid can be an Illinois ZIP
// and an hour off. Lines that cut through a state are not helped by this;
// exact boundaries would fix both.
//
// Unbounded, because in Alaska, Nevada or West Texas the nearest ZIP can be
// well past 50km and the nearest one in the same state is still the best
// answer. The KNN walk stops at the first same-state ZIP, about 1ms. It only
// runs long for a state with no ZIPs loaded at all, which a full load does not
// have.
const nearestZipInStateTimezoneSQL = `
	SELECT z.timezone FROM zip_codes z
	WHERE z.timezone <> '' AND z.state_code = $3
	ORDER BY z.geog <-> ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography
	LIMIT 1`

// describeAddresses fills in what every address result states about itself:
// its accuracy, and its timezone.
//
// One query for the whole page. The zone comes from the address's own ZIP
// first, since that is what the postal record says, and from the nearest ZIP
// centroid in its state, within timezoneSearchMeters, only when the postcode
// is missing or unknown.
//
// ZIP data is optional -- a deployment can serve addresses without it -- so a
// missing zip_codes leaves the timezones null rather than failing a search
// that otherwise succeeded. Before migration 21 adds zip_codes.geog, only the
// nearest-ZIP fallback is unavailable.
func describeAddresses(q querier, addrs []models.OhioAddress) error {
	if len(addrs) == 0 {
		return nil
	}
	ids := make([]int64, len(addrs))
	for i := range addrs {
		addrs[i].Accuracy = models.AccuracyPoint
		ids[i] = addrs[i].ID
	}

	// The zone polygons first, where they are loaded. Without this an address
	// would keep the ZIP-level answer while the same request's /reverse point
	// carried the exact one, and the two would disagree at precisely the
	// places the polygons exist to get right.
	zones, err := addressZonesFromBoundaries(q, ids)
	if err != nil {
		return err
	}

	rows, err := q.Query(`
		SELECT a.id, COALESCE(
			(SELECT z.timezone FROM zip_codes z
			 WHERE z.zip_code = left(a.postcode, 5) AND z.timezone <> ''),
			(SELECT z.timezone FROM zip_codes z
			 WHERE z.timezone <> '' AND z.state_code = a.region
			   AND ST_DWithin(z.geog, a.geom::geography, $2, false)
			 ORDER BY z.geog <-> a.geom::geography
			 LIMIT 1))
		FROM ohio_addresses a
		WHERE a.id = ANY($1)
	`, pq.Array(ids), timezoneSearchMeters)
	if isUndefinedColumn(err) {
		// Without geog only the fallback is lost; an address's own ZIP needs
		// nothing from migration 21.
		rows, err = q.Query(`
			SELECT a.id, z.timezone
			FROM ohio_addresses a
			JOIN zip_codes z ON z.zip_code = left(a.postcode, 5) AND z.timezone <> ''
			WHERE a.id = ANY($1)
		`, pq.Array(ids))
	}
	if err != nil {
		if isUndefinedTable(err) {
			return nil
		}
		return fmt.Errorf("failed to look up address timezones: %w", err)
	}
	defer rows.Close()

	zipZones := make(map[int64]string, len(addrs))
	for rows.Next() {
		var id int64
		var zone *string
		if err := rows.Scan(&id, &zone); err != nil {
			return fmt.Errorf("failed to scan address timezone: %w", err)
		}
		if zone != nil {
			zipZones[id] = *zone
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed to read address timezones: %w", err)
	}

	now := time.Now()
	for i := range addrs {
		if zone, ok := zones[addrs[i].ID]; ok {
			addrs[i].Timezone = &zone
			addrs[i].TimezoneSource = TimezoneBoundarySource
		} else if zone, ok := zipZones[addrs[i].ID]; ok {
			addrs[i].Timezone = &zone
			addrs[i].TimezoneSource = TimezoneZIPSource
		}
		if addrs[i].Timezone != nil {
			addrs[i].TimezoneDetails = timezoneDetails(*addrs[i].Timezone, now)
		}
	}
	return nil
}

// addressZonesFromBoundaries reads the containing zone polygon for a page of
// addresses. Empty when the timezone layer is not loaded, or before migration
// 26 creates the tables, which leaves the ZIP fallback to answer.
func addressZonesFromBoundaries(q querier, ids []int64) (map[int64]string, error) {
	zones := map[int64]string{}
	rows, err := q.Query(`
		SELECT DISTINCT ON (a.id) a.id, b.geoid
		FROM ohio_addresses a
		JOIN boundaries b ON b.layer = 'timezone' AND ST_Covers(b.geom, a.geom)
		WHERE a.id = ANY($1)
		  AND EXISTS (SELECT 1 FROM boundary_loads l
		              WHERE l.layer = 'timezone' AND l.state_fips = $2 AND l.available)
		ORDER BY a.id, b.geoid
	`, pq.Array(ids), NationalScope)
	if err != nil {
		if isUndefinedTable(err) {
			return zones, nil
		}
		return nil, fmt.Errorf("failed to look up address timezone boundaries: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var zone string
		if err := rows.Scan(&id, &zone); err != nil {
			return nil, fmt.Errorf("failed to scan an address timezone boundary: %w", err)
		}
		zones[id] = zone
	}
	return zones, rows.Err()
}

// timezoneFromZipCentroids is the approximate zone at a point: the zone of
// the nearest ZIP in its state, or within
// timezoneSearchMeters when stateCode is empty because the point is in none.
// Nil when there is no such ZIP, or before migration 21 adds zip_codes.geog.
func timezoneFromZipCentroids(db *sql.DB, lat, lng float64, stateCode string) (*string, error) {
	var zone string
	var err error
	if stateCode != "" {
		err = db.QueryRow(nearestZipInStateTimezoneSQL, lng, lat, stateCode).Scan(&zone)
	} else {
		err = db.QueryRow(nearestZipTimezoneSQL, lng, lat, timezoneSearchMeters).Scan(&zone)
	}
	if err == sql.ErrNoRows || isUndefinedColumn(err) || isUndefinedTable(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to find the timezone: %w", err)
	}
	return &zone, nil
}

// timezoneAtPoint is the zone at a point, and where that answer came from.
//
// The timezone layer, when loaded, is the real boundary: it puts the line
// where the line is, including the ones that run through a state, which the
// ZIP fallback cannot. The fallback still answers when the layer is not
// loaded, and for a point outside every zone -- the polygons stop at the
// coast, so a point at sea has none.
func timezoneAtPoint(db *sql.DB, lat, lng float64, stateCode string) (*string, string, error) {
	var zone string
	err := db.QueryRow(`
		SELECT b.geoid FROM boundaries b
		WHERE b.layer = 'timezone'
		  AND ST_Covers(b.geom, ST_SetSRID(ST_MakePoint($1, $2), 4326))
		  AND EXISTS (SELECT 1 FROM boundary_loads l
		              WHERE l.layer = 'timezone' AND l.state_fips = $3 AND l.available)
		ORDER BY b.geoid
		LIMIT 1
	`, lng, lat, NationalScope).Scan(&zone)
	switch {
	case err == nil:
		return &zone, TimezoneBoundarySource, nil
	case err != sql.ErrNoRows && !isUndefinedTable(err):
		return nil, "", fmt.Errorf("failed to find the timezone boundary: %w", err)
	}

	fallback, err := timezoneFromZipCentroids(db, lat, lng, stateCode)
	if err != nil || fallback == nil {
		return nil, "", err
	}
	return fallback, TimezoneZIPSource, nil
}
