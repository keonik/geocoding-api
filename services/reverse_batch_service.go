package services

import (
	"fmt"
	"time"

	"geocoding-api/database"
	"geocoding-api/models"

	"github.com/lib/pq"
)

// ReverseBatchItem is one coordinate to resolve.
type ReverseBatchItem struct {
	// ID is echoed back untouched so a caller can match answers to inputs
	// without relying on ordering. Optional.
	ID  string  `json:"id,omitempty"`
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// ReverseBatchResult is one answer, in the same position as its item.
type ReverseBatchResult struct {
	ID    string `json:"id,omitempty"`
	Found bool   `json:"found"`
	Error string `json:"error,omitempty"`
	*ReverseResult
}

// ReverseBatchResponse carries the answers and what the request cost.
type ReverseBatchResponse struct {
	Results []ReverseBatchResult `json:"results"`
	Total   int                  `json:"total"`
	Found   int                  `json:"found"`
	// BillableUnits is one per item, as on /geocode/batch: a batch of a
	// hundred coordinates is a hundred lookups however few requests carry it.
	BillableUnits      int     `json:"billable_units"`
	SearchRadiusMeters float64 `json:"search_radius_meters"`
}

// ReverseGeocodeBatch resolves many coordinates in one request.
//
// The point of a batch endpoint is the round trips it saves, and answering
// each item with the five queries /reverse runs would give that back: a
// hundred items would be five hundred queries. Each concern is asked once for
// the whole batch instead -- nearest address, containing county, containing
// state, nearest ZIP, timezone -- so the work is five queries whatever the
// batch size, and each still degrades on its own when the data behind it is
// not loaded.
//
// A bad coordinate fails its own item rather than the request: one caller
// typo in item 40 should not throw away the other 99 answers.
func ReverseGeocodeBatch(db querier, items []ReverseBatchItem, radiusMeters float64) (*ReverseBatchResponse, error) {
	if db == nil {
		db = database.DB
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("batch is empty")
	}
	if len(items) > MaxBatchItems {
		return nil, fmt.Errorf("batch has %d items, more than the %d allowed", len(items), MaxBatchItems)
	}

	if radiusMeters <= 0 {
		radiusMeters = defaultReverseRadiusMeters
	}
	if radiusMeters > maxReverseRadiusMeters {
		radiusMeters = maxReverseRadiusMeters
	}

	resp := &ReverseBatchResponse{
		Results:            make([]ReverseBatchResult, len(items)),
		Total:              len(items),
		BillableUnits:      len(items),
		SearchRadiusMeters: radiusMeters,
	}

	// Positions of the items worth querying for, and their coordinates. An
	// item rejected here contributes nothing to the queries below.
	var at []int
	var lngs, lats []float64
	for i, item := range items {
		resp.Results[i].ID = item.ID
		switch {
		case item.Lat < -90 || item.Lat > 90:
			resp.Results[i].Error = fmt.Sprintf("latitude %g is outside -90..90", item.Lat)
		case item.Lng < -180 || item.Lng > 180:
			resp.Results[i].Error = fmt.Sprintf("longitude %g is outside -180..180", item.Lng)
		default:
			resp.Results[i].ReverseResult = &ReverseResult{
				Lat: item.Lat, Lng: item.Lng, SearchRadiusMeters: radiusMeters,
			}
			at = append(at, i)
			lngs = append(lngs, item.Lng)
			lats = append(lats, item.Lat)
		}
	}

	if len(at) > 0 {
		if err := resolveReverseBatch(db, resp, at, lngs, lats, radiusMeters); err != nil {
			return nil, err
		}
	}

	for i := range resp.Results {
		if resolvedSomewhere(resp.Results[i].ReverseResult) {
			resp.Results[i].Found = true
			resp.Found++
		}
	}
	return resp, nil
}

// pointsCTE turns the batch's coordinates into a table of numbered points, so
// every query below joins against them rather than running per item.
//
// WITH ORDINALITY numbers the rows in array order, which is how each answer
// finds its way back to the item that asked for it.
const pointsCTE = `
	WITH points AS (
		SELECT n, ST_SetSRID(ST_MakePoint(lng, lat), 4326) AS geom
		FROM unnest($1::float8[], $2::float8[]) WITH ORDINALITY AS t(lng, lat, n)
	)`

// resolveReverseBatch fills in every answer, one query per concern.
func resolveReverseBatch(db querier, resp *ReverseBatchResponse, at []int, lngs, lats []float64, radius float64) error {
	points := []interface{}{pq.Array(lngs), pq.Array(lats)}

	// Nearest address. LATERAL runs the same KNN walk /reverse does, once per
	// point, inside one statement.
	rows, err := db.Query(pointsCTE+`
		SELECT p.n, `+addressColumns+`,
		       ST_Y(a.geom), ST_X(a.geom), a.created_at,
		       ST_Distance(a.geom::geography, p.geom::geography, false)
		FROM points p
		JOIN LATERAL (
			SELECT * FROM ohio_addresses a
			WHERE ST_DWithin(a.geom::geography, p.geom::geography, $3, false)
			ORDER BY a.geom <-> p.geom
			LIMIT 1
		) a ON true`, append(points, radius)...)
	if err != nil {
		return fmt.Errorf("failed to find the nearest addresses: %w", err)
	}
	addresses := map[int]*NearestAddress{}
	for rows.Next() {
		var n int
		var a NearestAddress
		if err := rows.Scan(&n, &a.ID, &a.Hash, &a.HouseNumber, &a.Street, &a.Unit, &a.City,
			&a.District, &a.Region, &a.Postcode, &a.County, &a.FullAddress,
			&a.Latitude, &a.Longitude, &a.CreatedAt, &a.DistanceMeters); err != nil {
			rows.Close()
			return fmt.Errorf("failed to read a nearest address: %w", err)
		}
		found := a
		addresses[n] = &found
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed to read the nearest addresses: %w", err)
	}

	// The addresses carry their accuracy and timezone like every other address
	// result, described in one query for the whole batch.
	described := make([]models.OhioAddress, 0, len(addresses))
	order := make([]int, 0, len(addresses))
	for n, a := range addresses {
		described = append(described, a.OhioAddress)
		order = append(order, n)
	}
	if err := describeAddresses(db, described); err != nil {
		return err
	}
	for i, n := range order {
		addresses[n].OhioAddress = described[i]
	}

	counties, err := containingValues(db, points, `
		SELECT p.n, c.county_name
		FROM points p
		JOIN LATERAL (
			SELECT county_name FROM ohio_counties
			WHERE bounds_geometry IS NOT NULL AND ST_Contains(bounds_geometry, p.geom)
			LIMIT 1
		) c ON true`)
	if err != nil {
		return fmt.Errorf("failed to find the containing counties: %w", err)
	}

	states := map[int]*ReverseState{}
	stateRows, err := db.Query(pointsCTE+`
		SELECT p.n, s.state_abbr, s.state_name
		FROM points p
		JOIN LATERAL (
			SELECT state_abbr, state_name FROM us_states
			WHERE geometry IS NOT NULL AND ST_Contains(geometry, p.geom)
			LIMIT 1
		) s ON true`, points...)
	if err != nil {
		return fmt.Errorf("failed to find the containing states: %w", err)
	}
	for stateRows.Next() {
		var n int
		var st ReverseState
		if err := stateRows.Scan(&n, &st.Code, &st.Name); err != nil {
			stateRows.Close()
			return fmt.Errorf("failed to read a containing state: %w", err)
		}
		found := st
		states[n] = &found
	}
	stateRows.Close()
	if err := stateRows.Err(); err != nil {
		return fmt.Errorf("failed to read the containing states: %w", err)
	}

	zips, err := nearestZipsBatch(db, points)
	if err != nil {
		return err
	}

	// Assign, then fill in each point's timezone from what was found.
	for i, position := range at {
		n := i + 1 // WITH ORDINALITY counts from one
		result := resp.Results[position].ReverseResult
		result.Address = addresses[n]
		result.Zip = zips[n]
		if county, ok := counties[n]; ok {
			result.County = &county
		}
		result.State = states[n]
	}

	return fillBatchTimezones(db, resp, at, lngs, lats)
}

// statefulPointsCTE is pointsCTE with each point's state alongside it, for the
// lookups that are restricted to it.
const statefulPointsCTE = `
	WITH points AS (
		SELECT n, ST_SetSRID(ST_MakePoint(lng, lat), 4326) AS geom, state
		FROM unnest($1::float8[], $2::float8[], $3::text[]) WITH ORDINALITY AS t(lng, lat, state, n)
	)`

// containingValues runs a one-column LATERAL lookup over the batch's points.
func containingValues(db querier, points []interface{}, query string) (map[int]string, error) {
	values := map[int]string{}
	rows, err := db.Query(pointsCTE+query, points...)
	if err != nil {
		if isUndefinedTable(err) {
			return values, nil
		}
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var n int
		var value string
		if err := rows.Scan(&n, &value); err != nil {
			return nil, err
		}
		values[n] = value
	}
	return values, rows.Err()
}

// nearestZipsBatch finds the nearest ZIP centroid to each point.
//
// Degrades like /reverse's own lookup: zip_codes.geog arrives with migration
// 21, and everything else in the response stands without it.
func nearestZipsBatch(db querier, points []interface{}) (map[int]*NearestZip, error) {
	zips := map[int]*NearestZip{}
	rows, err := db.Query(pointsCTE+`
		SELECT p.n, z.zip_code, z.city_name, z.state_code, z.timezone,
		       ST_Distance(z.geog, p.geom::geography, false)
		FROM points p
		JOIN LATERAL (
			SELECT * FROM zip_codes
			ORDER BY geog <-> p.geom::geography
			LIMIT 1
		) z ON true`, points...)
	if err != nil {
		if isUndefinedColumn(err) || isUndefinedTable(err) {
			return zips, nil
		}
		return nil, fmt.Errorf("failed to find the nearest ZIP codes: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		z := NearestZip{Accuracy: models.AccuracyPostalCentroid}
		var n int
		if err := rows.Scan(&n, &z.ZipCode, &z.CityName, &z.StateCode, &z.Timezone, &z.DistanceMeters); err != nil {
			return nil, fmt.Errorf("failed to read a nearest ZIP code: %w", err)
		}
		found := z
		zips[n] = &found
	}
	return zips, rows.Err()
}

// fillBatchTimezones gives each point the zone at it, in a fixed number of
// queries however many points there are.
//
// Three at most: the exact zone polygons for the whole batch, then the
// ZIP-derived fallback for whatever they did not cover -- split into the
// points that know their state and the points that do not, because those two
// need different queries. Doing the fallback per point instead cost two
// queries each, which is what a batch endpoint exists to avoid: measured at
// 107 queries for 50 items before this, and 9 after.
func fillBatchTimezones(db querier, resp *ReverseBatchResponse, at []int, lngs, lats []float64) error {
	points := []interface{}{pq.Array(lngs), pq.Array(lats)}
	zones, err := containingValues(db, points, `
		SELECT p.n, b.geoid
		FROM points p
		JOIN LATERAL (
			SELECT geoid FROM boundaries
			WHERE layer = 'timezone' AND ST_Covers(geom, p.geom)
			ORDER BY geoid
			LIMIT 1
		) b ON true
		WHERE EXISTS (SELECT 1 FROM boundary_loads l
		              WHERE l.layer = 'timezone' AND l.state_fips = '`+NationalScope+`' AND l.available)`)
	if err != nil {
		return fmt.Errorf("failed to find the timezone boundaries: %w", err)
	}

	now := time.Now()
	// What the polygons answered, and what is left for the fallback.
	var withState, withoutState []int
	for i, position := range at {
		n := i + 1 // WITH ORDINALITY counts from one
		result := resp.Results[position].ReverseResult
		if zone, ok := zones[n]; ok {
			result.Timezone = &zone
			result.TimezoneSource = TimezoneBoundarySource
			result.TimezoneDetails = timezoneDetails(zone, now)
			continue
		}
		if result.State != nil {
			withState = append(withState, position)
		} else {
			withoutState = append(withoutState, position)
		}
	}

	for _, group := range []struct {
		positions []int
		inState   bool
	}{{withState, true}, {withoutState, false}} {
		if len(group.positions) == 0 {
			continue
		}
		fallback, err := zipZonesForPoints(db, resp, group.positions, group.inState)
		if err != nil {
			return err
		}
		for _, position := range group.positions {
			result := resp.Results[position].ReverseResult
			if zone, ok := fallback[position]; ok {
				zone := zone
				result.Timezone = &zone
				result.TimezoneSource = TimezoneZIPSource
				result.TimezoneDetails = timezoneDetails(zone, now)
			}
		}
	}
	return nil
}

// zipZonesForPoints is the ZIP-derived zone for a group of points, in one
// query.
//
// inState says which query: a point inside a state takes the nearest ZIP in
// that state, unbounded, and a point in none takes the nearest within
// timezoneSearchMeters. They are separate statements rather than one with a
// CASE because a CASE hides the distance bound from the index -- the same
// mistake, measured at 556ms against 0.5ms, that timezoneSearchMeters
// documents.
func zipZonesForPoints(db querier, resp *ReverseBatchResponse, positions []int, inState bool) (map[int]string, error) {
	lngs := make([]float64, len(positions))
	lats := make([]float64, len(positions))
	states := make([]string, len(positions))
	for i, position := range positions {
		r := resp.Results[position].ReverseResult
		lngs[i], lats[i] = r.Lng, r.Lat
		if r.State != nil {
			states[i] = r.State.Code
		}
	}

	query := `
		SELECT p.n, z.timezone
		FROM points p
		JOIN LATERAL (
			SELECT timezone FROM zip_codes z
			WHERE z.timezone <> '' AND z.state_code = p.state
			ORDER BY z.geog <-> p.geom::geography
			LIMIT 1
		) z ON true`
	args := []interface{}{pq.Array(lngs), pq.Array(lats), pq.Array(states)}
	if !inState {
		query = `
		SELECT p.n, z.timezone
		FROM points p
		JOIN LATERAL (
			SELECT timezone FROM zip_codes z
			WHERE z.timezone <> ''
			  AND ST_DWithin(z.geog, p.geom::geography, $4, false)
			ORDER BY z.geog <-> p.geom::geography
			LIMIT 1
		) z ON true`
		args = append(args, timezoneSearchMeters)
	}

	rows, err := db.Query(statefulPointsCTE+query, args...)
	if err != nil {
		if isUndefinedColumn(err) || isUndefinedTable(err) {
			// Before migration 21 there is no geog to order by, and a
			// deployment may carry no ZIP data at all.
			return map[int]string{}, nil
		}
		return nil, fmt.Errorf("failed to find the nearest ZIP timezones: %w", err)
	}
	defer rows.Close()

	byPosition := map[int]string{}
	for rows.Next() {
		var n int
		var zone string
		if err := rows.Scan(&n, &zone); err != nil {
			return nil, fmt.Errorf("failed to read a nearest ZIP timezone: %w", err)
		}
		if n >= 1 && n <= len(positions) {
			byPosition[positions[n-1]] = zone
		}
	}
	return byPosition, rows.Err()
}

// resolvedSomewhere is whether an answer amounts to a place.
//
// A containing county or state, or an address inside the search radius, is one.
// The nearest ZIP is not on its own: that lookup is unbounded -- it reports the
// nearest centroid and how far away it is, which for a point in the Atlantic is
// a real ZIP four thousand kilometres away. The field stays, because /reverse
// returns it and the two must agree, but it only counts as a hit within the
// radius a caller could plausibly mean.
//
// So a caller can total the hits instead of comparing five fields and deciding
// for themselves what an ocean looks like.
func resolvedSomewhere(r *ReverseResult) bool {
	if r == nil {
		return false
	}
	if r.Address != nil || r.County != nil || r.State != nil {
		return true
	}
	return r.Zip != nil && r.Zip.DistanceMeters <= maxReverseRadiusMeters
}
