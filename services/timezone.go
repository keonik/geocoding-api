package services

import (
	"fmt"

	"geocoding-api/models"

	"github.com/lib/pq"
)

// timezoneSearchMeters bounds the nearest-ZIP search when the point is in no
// known state -- open water, mostly -- where a distant centroid is no evidence
// at all and would hand a point mid-lake the zone of whichever shore is
// nearest. Inside a known state there is no bound: in Alaska, Nevada or West
// Texas the nearest ZIP can be well past 50km, and the nearest one in the same
// state is still the best answer available.
const timezoneSearchMeters = 50000.0

// nearestZipTimezoneSQL finds the zone of the nearest ZIP centroid to a point.
//
// Restricted to one state when the state is known, because many zone lines are
// state lines: just inside Indiana, across from Illinois, the nearest centroid
// can be an Illinois ZIP and an hour off. Lines that cut through a state are
// not helped by this; exact boundaries would fix both.
// $1 and $2 are longitude and latitude, $3 the state code or empty, $4 the
// search radius in metres, applied only when $3 is empty.
const nearestZipTimezoneSQL = `
	SELECT z.timezone FROM zip_codes z
	WHERE z.timezone <> ''
	  AND CASE WHEN $3::text = ''
	           THEN ST_DWithin(z.geog, ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography, $4, false)
	           ELSE z.state_code = $3::text END
	ORDER BY z.geog <-> ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography
	LIMIT 1`

// describeAddresses fills in what every address result states about itself:
// its accuracy, and its timezone.
//
// One query for the whole page. The zone comes from the address's own ZIP
// first, since that is what the postal record says, and from the nearest ZIP
// centroid in its state only when the postcode is missing or unknown. Every
// address has a state, so the fallback is unbounded; see timezoneSearchMeters.
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
			 ORDER BY z.geog <-> a.geom::geography
			 LIMIT 1))
		FROM ohio_addresses a
		WHERE a.id = ANY($1)
	`, pq.Array(ids))
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
