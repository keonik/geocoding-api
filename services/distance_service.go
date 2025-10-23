package services

import (
	"fmt"
	"math"

	"geocoding-api/database"
	"geocoding-api/models"
)

// DistanceResponse represents the response for distance calculations
type DistanceResponse struct {
	FromZipCode  string  `json:"from_zip_code"`
	ToZipCode    string  `json:"to_zip_code"`
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
		DistanceKm:    distanceMiles * 1.60934, // Convert miles to kilometers
	}, nil
}

// FindZipCodesWithinRadius finds all ZIP codes within a specified radius of a center ZIP code
func FindZipCodesWithinRadius(centerZip string, radiusMiles float64, limit int) ([]*RadiusSearchResult, error) {
	// Get center ZIP code coordinates
	centerZipCode, err := GetZipCodeByZip(centerZip)
	if err != nil {
		return nil, fmt.Errorf("failed to get center ZIP code: %w", err)
	}
	if centerZipCode == nil {
		return nil, fmt.Errorf("center ZIP code %s not found", centerZip)
	}

	// Calculate bounding box for efficient querying
	// This creates a rough square around the center point to limit database results
	latDelta := radiusMiles / 69.0 // Approximate miles per degree of latitude
	lngDelta := radiusMiles / (69.0 * math.Cos(centerZipCode.Latitude*math.Pi/180.0)) // Adjust for longitude

	minLat := centerZipCode.Latitude - latDelta
	maxLat := centerZipCode.Latitude + latDelta
	minLng := centerZipCode.Longitude - lngDelta
	maxLng := centerZipCode.Longitude + lngDelta

	// Query database with bounding box filter
	query := `
		SELECT zip_code, city_name, state_code, state_name, zcta, zcta_parent,
			   population, density, primary_county_code, primary_county_name,
			   county_weights, county_names, county_codes, imprecise, military,
			   timezone, latitude, longitude
		FROM zip_codes
		WHERE latitude BETWEEN $1 AND $2
		  AND longitude BETWEEN $3 AND $4
		  AND zip_code != $5
		ORDER BY 
			(latitude - $6) * (latitude - $6) + (longitude - $7) * (longitude - $7)
		LIMIT $8
	`

	rows, err := database.DB.Query(query, minLat, maxLat, minLng, maxLng, centerZip, 
		centerZipCode.Latitude, centerZipCode.Longitude, limit*3) // Get more than needed for precise filtering
	if err != nil {
		return nil, fmt.Errorf("failed to query ZIP codes: %w", err)
	}
	defer rows.Close()

	var results []*RadiusSearchResult
	for rows.Next() {
		zc := &models.ZipCode{}
		err := rows.Scan(
			&zc.ZipCode, &zc.CityName, &zc.StateCode, &zc.StateName, &zc.ZCTA, &zc.ZCTAParent,
			&zc.Population, &zc.Density, &zc.PrimaryCountyCode, &zc.PrimaryCountyName,
			&zc.CountyWeights, &zc.CountyNames, &zc.CountyCodes, &zc.Imprecise, &zc.Military,
			&zc.Timezone, &zc.Latitude, &zc.Longitude,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan ZIP code: %w", err)
		}

		// Calculate precise distance using Haversine formula
		distance := haversineDistance(
			centerZipCode.Latitude, centerZipCode.Longitude,
			zc.Latitude, zc.Longitude,
		)

		// Only include if within the specified radius
		if distance <= radiusMiles {
			results = append(results, &RadiusSearchResult{
				ZipCode:       zc,
				DistanceMiles: distance,
				DistanceKm:    distance * 1.60934,
			})

			// Stop if we've reached the limit
			if len(results) >= limit {
				break
			}
		}
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
	const earthRadiusMiles = 3959.0

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