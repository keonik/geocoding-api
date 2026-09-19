package services

import (
	"database/sql"
	"fmt"

	"geocoding-api/database"
	"geocoding-api/models"
)

// defaultReverseRadiusMeters bounds the nearest-address search.
//
// Without a bound, a point in the middle of Lake Erie returns the nearest
// address on shore and reports it as "the" address there, which is worse than
// returning nothing: a caller has no way to tell a match from a shrug. 2km is
// generous for a genuine lookup and short enough that a miss reads as a miss.
const defaultReverseRadiusMeters = 2000.0

// maxReverseRadiusMeters caps what a caller may ask for. The search is a KNN
// walk off the GIST index, so a huge radius is not slow -- it is just
// meaningless, and returning an address 50km away invites it to be trusted.
const maxReverseRadiusMeters = 50000.0

// NearestAddress is the closest address to a point, with how far away it is.
type NearestAddress struct {
	models.OhioAddress
	DistanceMeters float64 `json:"distance_meters"`
}

// NearestZip is the closest ZIP code centroid.
type NearestZip struct {
	ZipCode        string          `json:"zip_code"`
	CityName       string          `json:"city_name"`
	StateCode      string          `json:"state_code"`
	Timezone       string          `json:"timezone"`
	Accuracy       models.Accuracy `json:"accuracy"`
	DistanceMeters float64         `json:"distance_meters"`
}

// ReverseResult answers "what is here".
//
// Every field is independent. A point can be inside a county and a state while
// having no address within range -- rural Ohio, a lake, a state where only ZIP
// data is loaded -- and saying so is more useful than forcing a single answer.
// The address is the part that can legitimately be absent.
type ReverseResult struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`

	Address *NearestAddress `json:"address"`
	Zip     *NearestZip     `json:"zip"`
	County  *string         `json:"county"`
	State   *ReverseState   `json:"state"`

	// Timezone is the IANA zone at the queried point, from the nearest ZIP
	// centroid in the containing state within 50km. Null when there is none --
	// open water, or a state with no ZIP data. ZIP-level, so a point within a
	// few kilometres of a zone line inside one state (the Florida panhandle,
	// western Kentucky) can report the neighbouring zone.
	Timezone *string `json:"timezone"`

	SearchRadiusMeters float64 `json:"search_radius_meters"`
}

// ReverseState is the containing state, by boundary rather than by proximity.
type ReverseState struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// ReverseGeocode resolves a coordinate to the things that describe it.
//
// The counterpart to forward geocoding, and until now the API had only
// /states/lookup -- point to state and nothing else. Everything needed for the
// rest was already present: a GIST index on ohio_addresses.geom, county
// polygons, and the zip_codes geography column added in migration 21.
func ReverseGeocode(db *sql.DB, lat, lng, radiusMeters float64) (*ReverseResult, error) {
	if db == nil {
		db = database.DB
	}

	if lat < -90 || lat > 90 {
		return nil, fmt.Errorf("latitude %g is outside -90..90", lat)
	}
	if lng < -180 || lng > 180 {
		return nil, fmt.Errorf("longitude %g is outside -180..180", lng)
	}

	if radiusMeters <= 0 {
		radiusMeters = defaultReverseRadiusMeters
	}
	if radiusMeters > maxReverseRadiusMeters {
		radiusMeters = maxReverseRadiusMeters
	}

	result := &ReverseResult{Lat: lat, Lng: lng, SearchRadiusMeters: radiusMeters}

	if err := result.findAddress(db, lat, lng, radiusMeters); err != nil {
		return nil, err
	}
	if err := result.findCounty(db, lat, lng); err != nil {
		return nil, err
	}
	if err := result.findState(db, lat, lng); err != nil {
		return nil, err
	}
	if err := result.findZip(db, lat, lng); err != nil {
		return nil, err
	}
	// After findState: the zone lookup is restricted to the containing state.
	if err := result.findTimezone(db, lat, lng); err != nil {
		return nil, err
	}

	return result, nil
}

// findAddress walks the GIST index outward from the point.
//
// ST_DWithin bounds the search and the KNN operator orders it, so Postgres
// stops at the first hit rather than measuring every candidate. The distance is
// taken from the same geography comparison that admitted the row, so a result
// can never report a distance beyond the radius that found it.
func (r *ReverseResult) findAddress(db *sql.DB, lat, lng, radius float64) error {
	var a NearestAddress
	err := db.QueryRow(`
		SELECT id, hash, house_number, street, unit, city, district, region,
		       postcode, county, full_address,
		       ST_Y(geom), ST_X(geom), created_at,
		       ST_Distance(geom::geography, ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography, false)
		FROM ohio_addresses
		WHERE ST_DWithin(geom::geography, ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography, $3, false)
		ORDER BY geom <-> ST_SetSRID(ST_MakePoint($1, $2), 4326)
		LIMIT 1
	`, lng, lat, radius).Scan(
		&a.ID, &a.Hash, &a.HouseNumber, &a.Street, &a.Unit, &a.City, &a.District,
		&a.Region, &a.Postcode, &a.County, &a.FullAddress,
		&a.Latitude, &a.Longitude, &a.CreatedAt, &a.DistanceMeters,
	)
	if err == sql.ErrNoRows {
		// No address in range is an answer, not a failure.
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to find the nearest address: %w", err)
	}
	described := []models.OhioAddress{a.OhioAddress}
	if err := describeAddresses(db, described); err != nil {
		return err
	}
	a.OhioAddress = described[0]
	r.Address = &a
	return nil
}

// findCounty uses containment, not proximity: a point is in exactly one county
// or none, and the nearest county to a point outside every boundary is not a
// meaningful answer.
func (r *ReverseResult) findCounty(db *sql.DB, lat, lng float64) error {
	var county string
	err := db.QueryRow(`
		SELECT county_name FROM ohio_counties
		WHERE bounds_geometry IS NOT NULL
		  AND ST_Contains(bounds_geometry, ST_SetSRID(ST_MakePoint($1, $2), 4326))
		LIMIT 1
	`, lng, lat).Scan(&county)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to find the containing county: %w", err)
	}
	r.County = &county
	return nil
}

func (r *ReverseResult) findState(db *sql.DB, lat, lng float64) error {
	var code, name string
	err := db.QueryRow(`
		SELECT state_abbr, state_name FROM us_states
		WHERE geometry IS NOT NULL
		  AND ST_Contains(geometry, ST_SetSRID(ST_MakePoint($1, $2), 4326))
		LIMIT 1
	`, lng, lat).Scan(&code, &name)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to find the containing state: %w", err)
	}
	r.State = &ReverseState{Code: code, Name: name}
	return nil
}

// findZip is nearest-centroid, and says so in the distance.
//
// zip_codes holds a point per ZIP, not a boundary, so "the ZIP containing this
// coordinate" is not a question this data can answer. Reporting the nearest
// centroid with its distance is honest; silently presenting it as containment
// would not be.
func (r *ReverseResult) findZip(db *sql.DB, lat, lng float64) error {
	z := NearestZip{Accuracy: models.AccuracyPostalCentroid}
	err := db.QueryRow(`
		SELECT zip_code, city_name, state_code, timezone,
		       ST_Distance(geog, ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography, false)
		FROM zip_codes
		ORDER BY geog <-> ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography
		LIMIT 1
	`, lng, lat).Scan(&z.ZipCode, &z.CityName, &z.StateCode, &z.Timezone, &z.DistanceMeters)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		// zip_codes.geog arrives with migration 21. Everything else in the
		// response still stands without it, so a missing column degrades this
		// field rather than failing the request.
		if isUndefinedColumn(err) {
			return nil
		}
		return fmt.Errorf("failed to find the nearest ZIP code: %w", err)
	}
	r.Zip = &z
	return nil
}

// findTimezone reports the zone at the point itself, which is not necessarily
// the nearest ZIP's: that one is found without regard to state, and across a
// state line it is often in the other zone.
func (r *ReverseResult) findTimezone(db *sql.DB, lat, lng float64) error {
	state := ""
	if r.State != nil {
		state = r.State.Code
	}
	var zone string
	err := db.QueryRow(nearestZipTimezoneSQL, lng, lat, state, timezoneSearchMeters).Scan(&zone)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		// Same degradation as findZip: geog arrives with migration 21.
		if isUndefinedColumn(err) {
			return nil
		}
		return fmt.Errorf("failed to find the timezone: %w", err)
	}
	r.Timezone = &zone
	return nil
}
