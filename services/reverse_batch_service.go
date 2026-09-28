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

	// The items worth querying for. One rejected here contributes nothing to
	// the queries below, so the queries see a shorter list than the caller
	// sent -- which is why each point remembers which result it belongs to
	// rather than being matched back by its position in the request.
	var points []batchPoint
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
			points = append(points, batchPoint{at: i, lng: item.Lng, lat: item.Lat})
		}
	}

	if len(points) > 0 {
		if err := resolveReverseBatch(db, resp, points, radiusMeters); err != nil {
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

// batchPoint is one coordinate being resolved, and where its answer belongs.
//
// The alternative -- parallel slices of positions, longitudes and latitudes --
// is what made the mapping from query rows back to results hard to audit, and
// the ordinality arithmetic had to be repeated at each use.
type batchPoint struct {
	at       int // index into the response's results
	lng, lat float64
}

// ordinal is the point's position in the arrays handed to unnest, which
// numbers from one.
func (p batchPoint) ordinal(i int) int { return i + 1 }

// coordinateArrays renders points as the two arrays every query below takes.
func coordinateArrays(points []batchPoint) []interface{} {
	lngs := make([]float64, len(points))
	lats := make([]float64, len(points))
	for i, p := range points {
		lngs[i], lats[i] = p.lng, p.lat
	}
	return []interface{}{pq.Array(lngs), pq.Array(lats)}
}

// resolveReverseBatch fills in every answer, one query per concern.
func resolveReverseBatch(db querier, resp *ReverseBatchResponse, points []batchPoint, radius float64) error {
	coords := coordinateArrays(points)

	// Nearest address. LATERAL runs the same KNN walk /reverse does, once per
	// point, inside one statement.
	//
	// addressColumns is spliced in unqualified, so it must not name a column
	// the points CTE also has -- n or geom -- or the reference is ambiguous.
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
		) a ON true`, append(coords, radius)...)
	if err != nil {
		if isUndefinedTable(err) || isUndefinedColumn(err) {
			// No address data loaded. Every other field still answers, which
			// is how /reverse treats each of its lookups: a missing table
			// empties one field rather than the response.
			rows = nil
		} else {
			return fmt.Errorf("failed to find the nearest addresses: %w", err)
		}
	}
	addresses := map[int]*NearestAddress{}
	for rows != nil && rows.Next() {
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
	if rows != nil {
		rows.Close()
		if err := rows.Err(); err != nil {
			return fmt.Errorf("failed to read the nearest addresses: %w", err)
		}
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

	counties, err := containingValues(db, coords, "containing counties", `
		SELECT p.n, c.county_name
		FROM points p
		JOIN LATERAL (
			SELECT county_name FROM ohio_counties
			WHERE bounds_geometry IS NOT NULL AND ST_Contains(bounds_geometry, p.geom)
			LIMIT 1
		) c ON true`)
	if err != nil {
		return err
	}

	states := map[int]*ReverseState{}
	stateRows, err := db.Query(pointsCTE+`
		SELECT p.n, s.state_abbr, s.state_name
		FROM points p
		JOIN LATERAL (
			SELECT state_abbr, state_name FROM us_states
			WHERE geometry IS NOT NULL AND ST_Contains(geometry, p.geom)
			LIMIT 1
		) s ON true`, coords...)
	if err != nil {
		if isUndefinedTable(err) || isUndefinedColumn(err) {
			stateRows = nil
		} else {
			return fmt.Errorf("failed to find the containing states: %w", err)
		}
	}
	for stateRows != nil && stateRows.Next() {
		var n int
		var st ReverseState
		if err := stateRows.Scan(&n, &st.Code, &st.Name); err != nil {
			stateRows.Close()
			return fmt.Errorf("failed to read a containing state: %w", err)
		}
		found := st
		states[n] = &found
	}
	if stateRows != nil {
		stateRows.Close()
		if err := stateRows.Err(); err != nil {
			return fmt.Errorf("failed to read the containing states: %w", err)
		}
	}

	zips, err := nearestZipsBatch(db, coords)
	if err != nil {
		return err
	}

	// Assign, then fill in each point's timezone from what was found.
	for i, point := range points {
		n := point.ordinal(i)
		result := resp.Results[point.at].ReverseResult
		result.Address = addresses[n]
		result.Zip = zips[n]
		if county, ok := counties[n]; ok {
			result.County = &county
		}
		result.State = states[n]
	}

	return fillBatchTimezones(db, resp, points)
}

// statefulPointsCTE is pointsCTE with each point's state alongside it, for the
// lookups that are restricted to it.
const statefulPointsCTE = `
	WITH points AS (
		SELECT n, ST_SetSRID(ST_MakePoint(lng, lat), 4326) AS geom, state
		FROM unnest($1::float8[], $2::float8[], $3::text[]) WITH ORDINALITY AS t(lng, lat, state, n)
	)`

// containingValues runs a one-column LATERAL lookup over the batch's points.
//
// what names the thing being looked up, so a read fault is reported as one
// rather than inheriting the caller's "failed to find ..." wording.
func containingValues(db querier, points []interface{}, what, query string) (map[int]string, error) {
	values := map[int]string{}
	rows, err := db.Query(pointsCTE+query, points...)
	if err != nil {
		if isUndefinedTable(err) || isUndefinedColumn(err) {
			return values, nil
		}
		return nil, fmt.Errorf("failed to look up %s: %w", what, err)
	}
	defer rows.Close()
	for rows.Next() {
		var n int
		var value string
		if err := rows.Scan(&n, &value); err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", what, err)
		}
		values[n] = value
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", what, err)
	}
	return values, nil
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
func fillBatchTimezones(db querier, resp *ReverseBatchResponse, points []batchPoint) error {
	coords := coordinateArrays(points)
	zones, err := containingValues(db, coords, "timezone boundaries", `
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
		return err
	}

	now := time.Now()
	// What the polygons answered, and what is left for the fallback. The two
	// groups need different queries, so they are gathered rather than asked
	// point by point.
	var inState, stateless []batchPoint
	for i, point := range points {
		result := resp.Results[point.at].ReverseResult
		if zone, ok := zones[point.ordinal(i)]; ok {
			result.Timezone = &zone
			result.TimezoneSource = TimezoneBoundarySource
			result.TimezoneDetails = timezoneDetails(zone, now)
			continue
		}
		if result.State != nil {
			inState = append(inState, point)
		} else {
			stateless = append(stateless, point)
		}
	}

	for _, group := range []struct {
		points []batchPoint
		zones  func(querier, *ReverseBatchResponse, []batchPoint) (map[int]string, error)
	}{
		{inState, zipZonesInState},
		{stateless, zipZonesNearby},
	} {
		if len(group.points) == 0 {
			continue
		}
		fallback, err := group.zones(db, resp, group.points)
		if err != nil {
			return err
		}
		for _, point := range group.points {
			if zone, ok := fallback[point.at]; ok {
				zone := zone
				result := resp.Results[point.at].ReverseResult
				result.Timezone = &zone
				result.TimezoneSource = TimezoneZIPSource
				result.TimezoneDetails = timezoneDetails(zone, now)
			}
		}
	}
	return nil
}

// zipZonesInState is the nearest same-state ZIP's zone for each point, in one
// query. Unbounded, like the single-point lookup: inside a known state the
// nearest ZIP in it is the best answer at any distance.
func zipZonesInState(db querier, resp *ReverseBatchResponse, points []batchPoint) (map[int]string, error) {
	return zipZones(db, resp, points, `
		SELECT p.n, z.timezone
		FROM points p
		JOIN LATERAL (
			SELECT timezone FROM zip_codes z
			WHERE z.timezone <> '' AND z.state_code = p.state
			ORDER BY z.geog <-> p.geom::geography
			LIMIT 1
		) z ON true`)
}

// zipZonesNearby is the same for points in no state, where the search is
// bounded instead: a distant centroid is no evidence about a point at sea.
//
// A separate statement rather than a CASE inside one, because a CASE hides the
// distance bound from the index -- the mistake timezoneSearchMeters documents,
// measured at 556ms against 0.5ms.
func zipZonesNearby(db querier, resp *ReverseBatchResponse, points []batchPoint) (map[int]string, error) {
	return zipZones(db, resp, points, `
		SELECT p.n, z.timezone
		FROM points p
		JOIN LATERAL (
			SELECT timezone FROM zip_codes z
			WHERE z.timezone <> ''
			  AND ST_DWithin(z.geog, p.geom::geography, `+fmt.Sprintf("%g", timezoneSearchMeters)+`, false)
			ORDER BY z.geog <-> p.geom::geography
			LIMIT 1
		) z ON true`)
}

// zipZones runs one of those queries and keys the answers by the result each
// point belongs to.
func zipZones(db querier, resp *ReverseBatchResponse, points []batchPoint, query string) (map[int]string, error) {
	lngs := make([]float64, len(points))
	lats := make([]float64, len(points))
	states := make([]string, len(points))
	for i, point := range points {
		lngs[i], lats[i] = point.lng, point.lat
		if state := resp.Results[point.at].ReverseResult.State; state != nil {
			states[i] = state.Code
		}
	}

	rows, err := db.Query(statefulPointsCTE+query, pq.Array(lngs), pq.Array(lats), pq.Array(states))
	if err != nil {
		if isUndefinedColumn(err) || isUndefinedTable(err) {
			// Before migration 21 there is no geog to order by, and a
			// deployment may carry no ZIP data at all.
			return map[int]string{}, nil
		}
		return nil, fmt.Errorf("failed to look up nearest ZIP timezones: %w", err)
	}
	defer rows.Close()

	byResult := map[int]string{}
	for rows.Next() {
		var n int
		var zone string
		if err := rows.Scan(&n, &zone); err != nil {
			return nil, fmt.Errorf("failed to read a nearest ZIP timezone: %w", err)
		}
		if n >= 1 && n <= len(points) {
			byResult[points[n-1].at] = zone
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read nearest ZIP timezones: %w", err)
	}
	return byResult, nil
}

// resolvedSomewhere is whether an answer amounts to a place.
//
// A containing county or state, or an address inside the search radius, is one.
// The nearest ZIP is not on its own: that lookup is unbounded -- it reports the
// nearest centroid and how far away it is, which for a point in the Atlantic is
// a real ZIP four thousand kilometres away. The field stays, because /reverse
// returns it and the two must agree, but it only counts as a hit inside the
// radius the caller asked for, which is what the documentation promises.
//
// Measured against the cap rather than that radius, as this first did, a point
// 45km off the Florida coast came back found with every field null and a
// requested radius of one metre.
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
	return r.Zip != nil && r.Zip.DistanceMeters <= r.SearchRadiusMeters
}
