package services

import (
	"errors"
	"fmt"
	"log"
	"math"

	"geocoding-api/database"
	"geocoding-api/models"

	"github.com/lib/pq"
)

// metersPerMile is the international mile, exactly 1609.344 m. Radius search
// converts miles to meters for PostGIS and back again for the response.
const metersPerMile = 1609.344

// earthRadiusMiles is the mean Earth radius PostGIS uses for its sphere
// (6371008.771415 m), expressed in miles.
//
// It was 3959.0 until radius search moved onto PostGIS. Both numbers are
// defensible as "the" Earth radius, but only one of them makes /distance,
// /proximity and /nearby report the same mileage for the same pair of ZIPs,
// and disagreeing endpoints are worse than a rounding choice. The change moves
// every distance this package reports by 0.006% -- about 32 feet in 100 miles,
// far inside the error already introduced by treating a ZIP code as a point.
const earthRadiusMiles = 6371008.771415 / metersPerMile

// DistanceResponse represents the response for distance calculations
type DistanceResponse struct {
	FromZipCode   string  `json:"from_zip_code"`
	ToZipCode     string  `json:"to_zip_code"`
	DistanceMiles float64 `json:"distance_miles"`
	DistanceKm    float64 `json:"distance_km"`
}

// RadiusSearchResult represents a ZIP code with its distance from center
type RadiusSearchResult struct {
	ZipCode       *models.ZipCode `json:"zip_code"`
	DistanceMiles float64         `json:"distance_miles"`
	DistanceKm    float64         `json:"distance_km"`
}

// CalculateDistanceBetweenZipCodes calculates the distance between two ZIP codes
func CalculateDistanceBetweenZipCodes(fromZip, toZip string) (*DistanceResponse, error) {
	// Get coordinates for both ZIP codes
	fromZipCode, err := GetZipCodeByZip(fromZip)
	if err != nil {
		return nil, fmt.Errorf("failed to get from ZIP code: %w", err)
	}
	if fromZipCode == nil {
		return nil, fmt.Errorf("from ZIP code %s not found", fromZip)
	}

	toZipCode, err := GetZipCodeByZip(toZip)
	if err != nil {
		return nil, fmt.Errorf("failed to get to ZIP code: %w", err)
	}
	if toZipCode == nil {
		return nil, fmt.Errorf("to ZIP code %s not found", toZip)
	}

	// Calculate distance
	distanceMiles := haversineDistance(
		fromZipCode.Latitude, fromZipCode.Longitude,
		toZipCode.Latitude, toZipCode.Longitude,
	)

	return &DistanceResponse{
		FromZipCode:   fromZip,
		ToZipCode:     toZip,
		DistanceMiles: distanceMiles,
		DistanceKm:    distanceMiles * metersPerMile / 1000.0,
	}, nil
}

// FindZipCodesWithinRadius finds all ZIP codes within a specified radius of a
// center ZIP code, nearest first.
//
// This used to draw a lat/lng bounding box, pull LIMIT*3 rows ordered by
// squared degrees, then filter by Haversine in Go and stop at LIMIT. That was
// wrong in two ways, not merely slow:
//
//   - A bounding box circumscribes the circle, so up to 1 - pi/4 (21%) of its
//     area -- concentrated in the corners -- is outside the radius. When
//     enough of the LIMIT*3 candidates fell in those corners they consumed the
//     slice and were then discarded, and the call returned fewer ZIPs than
//     genuinely sat inside the radius. A silent wrong answer.
//   - Squared degrees is not distance. A degree of longitude is cos(latitude)
//     as long as a degree of latitude -- 0.68x at 47 deg N -- so the ordering
//     that decided which LIMIT*3 rows to keep was itself skewed, and skewed
//     worse the further from the equator.
//
// Now it is one indexed query. ST_DWithin does the filtering against the GIST
// index from migration 21, the KNN operator orders by true distance, and LIMIT
// applies in SQL to a set that is already correct.
//
// use_spheroid is false throughout, which asks PostGIS for great-circle
// distance on a sphere -- the same model as haversineDistance, so /nearby
// agrees with /distance and /proximity for any given pair. Leaving it at the
// default (true, WGS84 spheroid) would be more accurate in isolation but would
// disagree with those endpoints by ~0.25%, which is a quarter mile over a
// hundred. Reported distance comes from the same ST_Distance call the filter
// used, so a result can never come back reporting a distance above the radius
// that admitted it.
func FindZipCodesWithinRadius(centerZip string, radiusMiles float64, limit int) ([]*RadiusSearchResult, error) {
	// Confirm the center exists so a bad ZIP is a clear error rather than an
	// empty result set.
	centerZipCode, err := GetZipCodeByZip(centerZip)
	if err != nil {
		return nil, fmt.Errorf("failed to get center ZIP code: %w", err)
	}
	if centerZipCode == nil {
		return nil, fmt.Errorf("center ZIP code %s not found", centerZip)
	}

	rows, err := database.DB.Query(radiusQuery(true), centerZip, radiusMiles*metersPerMile, limit)
	if err != nil {
		if !isUndefinedColumn(err) {
			return nil, fmt.Errorf("failed to query ZIP codes: %w", err)
		}
		// zip_codes.geog does not exist yet. Migrations run asynchronously by
		// default (see main.go), so between a deploy and migration 21 landing
		// there is a window where the server is serving but the column is not
		// there. Failing here would make this endpoint 500 for that whole
		// window -- a regression the bounding-box implementation this replaced
		// could not have, because it needed no schema beyond latitude and
		// longitude.
		//
		// The fallback builds the same geographies inline from those columns.
		// Identical results and identical ordering; it just cannot use the
		// GIST index, so it scans. Correct and slow beats 500 for the minute
		// or two this is live.
		log.Printf("radius search: zip_codes.geog missing, falling back to an unindexed scan (migration 21 pending?)")
		rows, err = database.DB.Query(radiusQuery(false), centerZip, radiusMiles*metersPerMile, limit)
		if err != nil {
			return nil, fmt.Errorf("failed to query ZIP codes: %w", err)
		}
	}
	defer rows.Close()

	var results []*RadiusSearchResult
	for rows.Next() {
		zc := &models.ZipCode{}
		var distanceMeters float64
		err := rows.Scan(
			&zc.ZipCode, &zc.CityName, &zc.StateCode, &zc.StateName, &zc.ZCTA, &zc.ZCTAParent,
			&zc.Population, &zc.Density, &zc.PrimaryCountyCode, &zc.PrimaryCountyName,
			&zc.CountyWeights, &zc.CountyNames, &zc.CountyCodes, &zc.Imprecise, &zc.Military,
			&zc.Timezone, &zc.Latitude, &zc.Longitude,
			&distanceMeters,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan ZIP code: %w", err)
		}

		distanceMiles := distanceMeters / metersPerMile
		results = append(results, &RadiusSearchResult{
			ZipCode:       zc,
			DistanceMiles: distanceMiles,
			DistanceKm:    distanceMeters / 1000.0,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read ZIP code rows: %w", err)
	}

	return results, nil
}

// IsZipCodeWithinRadius checks if one ZIP code is within a specified radius of another
func IsZipCodeWithinRadius(centerZip, targetZip string, radiusMiles float64) (bool, float64, error) {
	distance, err := CalculateDistanceBetweenZipCodes(centerZip, targetZip)
	if err != nil {
		return false, 0, err
	}

	return distance.DistanceMiles <= radiusMiles, distance.DistanceMiles, nil
}

// haversineDistance calculates the distance between two points on Earth using the Haversine formula
// Returns distance in miles
func haversineDistance(lat1, lng1, lat2, lng2 float64) float64 {
	// Convert degrees to radians
	lat1Rad := lat1 * math.Pi / 180.0
	lng1Rad := lng1 * math.Pi / 180.0
	lat2Rad := lat2 * math.Pi / 180.0
	lng2Rad := lng2 * math.Pi / 180.0

	// Calculate differences
	deltaLat := lat2Rad - lat1Rad
	deltaLng := lng2Rad - lng1Rad

	// Haversine formula
	a := math.Sin(deltaLat/2)*math.Sin(deltaLat/2) +
		math.Cos(lat1Rad)*math.Cos(lat2Rad)*
			math.Sin(deltaLng/2)*math.Sin(deltaLng/2)

	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	// Distance in miles
	return earthRadiusMiles * c
}

// radiusQuery renders the radius search.
//
// indexed selects the stored geography column from migration 21, which the
// GIST index covers. The fallback recomputes the same value inline from
// longitude/latitude so the query still works before that migration has run.
// Both forms are the same geography, so results and ordering match exactly.
func radiusQuery(indexed bool) string {
	geog := "z.geog"
	centerGeog := "geog"
	if !indexed {
		geog = "ST_SetSRID(ST_MakePoint(z.longitude, z.latitude), 4326)::geography"
		centerGeog = "ST_SetSRID(ST_MakePoint(longitude, latitude), 4326)::geography"
	}

	return fmt.Sprintf(`
		WITH center AS (
			SELECT %s AS geog FROM zip_codes WHERE zip_code = $1
		)
		SELECT z.zip_code, z.city_name, z.state_code, z.state_name, z.zcta, z.zcta_parent,
			   z.population, z.density, z.primary_county_code, z.primary_county_name,
			   z.county_weights, z.county_names, z.county_codes, z.imprecise, z.military,
			   z.timezone, z.latitude, z.longitude,
			   ST_Distance(%s, center.geog, false) AS distance_meters
		FROM zip_codes z, center
		WHERE z.zip_code <> $1
		  AND ST_DWithin(%s, center.geog, $2, false)
		ORDER BY %s <-> center.geog
		LIMIT $3
	`, centerGeog, geog, geog, geog)
}

// isUndefinedColumn reports whether err is Postgres 42703, raised when a
// referenced column does not exist.
func isUndefinedColumn(err error) bool {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		return pqErr.Code == "42703"
	}
	return false
}
