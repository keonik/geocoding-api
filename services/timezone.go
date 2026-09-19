package services

import (
	"fmt"

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

	zones := make(map[int64]string, len(addrs))
	for rows.Next() {
		var id int64
		var zone *string
		if err := rows.Scan(&id, &zone); err != nil {
			return fmt.Errorf("failed to scan address timezone: %w", err)
		}
		if zone != nil {
			zones[id] = *zone
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed to read address timezones: %w", err)
	}

	for i := range addrs {
		if zone, ok := zones[addrs[i].ID]; ok {
			addrs[i].Timezone = &zone
		}
	}
	return nil
}
