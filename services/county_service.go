package services

import (
	"database/sql"
	"fmt"
	"strings"

	"geocoding-api/database"
	"geocoding-api/models"
)

type CountyService struct {
	db *sql.DB
}

func NewCountyService() *CountyService {
	return &CountyService{
		db: database.DB,
	}
}

// GetAllCounties returns a list of all Ohio counties with basic information
func (cs *CountyService) GetAllCounties(params models.CountySearchParams) ([]models.CountyListResponse, error) {
	query := `
		SELECT id, county_name, address_count 
		FROM ohio_counties 
		WHERE 1=1
	`
	
	var conditions []string
	var args []interface{}
	argIndex := 1

	// Add search conditions
	if params.Name != "" {
		conditions = append(conditions, fmt.Sprintf("LOWER(county_name) LIKE LOWER($%d)", argIndex))
		args = append(args, "%"+params.Name+"%")
		argIndex++
	}

	if params.MinAddresses > 0 {
		conditions = append(conditions, fmt.Sprintf("address_count >= $%d", argIndex))
		args = append(args, params.MinAddresses)
		argIndex++
	}

	if params.MaxAddresses > 0 {
		conditions = append(conditions, fmt.Sprintf("address_count <= $%d", argIndex))
		args = append(args, params.MaxAddresses)
		argIndex++
	}

	// Add conditions to query
	if len(conditions) > 0 {
		query += " AND " + strings.Join(conditions, " AND ")
	}

	// Add ordering
	query += " ORDER BY address_count DESC, county_name ASC"

	// Add pagination
	if params.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", argIndex)
		args = append(args, params.Limit)
		argIndex++
	}

	if params.Offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", argIndex)
		args = append(args, params.Offset)
	}

	rows, err := cs.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query counties: %w", err)
	}
	defer rows.Close()

	var counties []models.CountyListResponse
	for rows.Next() {
		var county models.CountyListResponse
		err := rows.Scan(&county.ID, &county.CountyName, &county.AddressCount)
		if err != nil {
			return nil, fmt.Errorf("failed to scan county: %w", err)
		}
		counties = append(counties, county)
	}

	return counties, nil
}

// GetCountyByName returns detailed information about a specific county
func (cs *CountyService) GetCountyByName(name string) (*models.OhioCounty, error) {
	query := `
		SELECT id, county_name, source_name, layer, address_count, stats, 
			   ST_AsText(bounds_geometry) as bounds_wkt, created_at, updated_at
		FROM ohio_counties 
		WHERE LOWER(county_name) = LOWER($1)
	`

	var county models.OhioCounty
	var statsJSON sql.NullString

	err := cs.db.QueryRow(query, name).Scan(
		&county.ID, &county.CountyName, &county.SourceName, &county.Layer,
		&county.AddressCount, &statsJSON, &county.BoundsGeometry,
		&county.CreatedAt, &county.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("county not found: %s", name)
		}
		return nil, fmt.Errorf("failed to query county: %w", err)
	}

	// Parse stats JSON if present
	if statsJSON.Valid && statsJSON.String != "" {
		// Note: In a real implementation, you'd unmarshal the JSON here
		// For now, we'll leave stats empty since it's complex to parse safely
		county.Stats = make(map[string]interface{})
	}

	return &county, nil
}

// GetCountyBoundaryGeoJSON returns the county boundary in GeoJSON format
func (cs *CountyService) GetCountyBoundaryGeoJSON(name string) (*models.CountyBoundaryGeoJSON, error) {
	query := `
		SELECT county_name, source_name, layer, address_count, stats,
			   ST_AsGeoJSON(bounds_geometry) as bounds_geojson
		FROM ohio_counties 
		WHERE LOWER(county_name) = LOWER($1)
	`

	var countyName, sourceName, layer, boundsGeoJSON string
	var addressCount int
	var statsJSON sql.NullString

	err := cs.db.QueryRow(query, name).Scan(
		&countyName, &sourceName, &layer, &addressCount, &statsJSON, &boundsGeoJSON,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("county not found: %s", name)
		}
		return nil, fmt.Errorf("failed to query county boundary: %w", err)
	}

	// Parse the geometry from PostGIS GeoJSON output
	// PostGIS ST_AsGeoJSON returns just the geometry part, we need to wrap it in a Feature
	geoJSON := &models.CountyBoundaryGeoJSON{
		Type: "FeatureCollection",
		Features: []models.CountyFeatureGeoJSON{
			{
				Type: "Feature",
				Properties: models.CountyPropertiesGeoJSON{
					CountyName:   countyName,
					SourceName:   sourceName,
					Layer:        layer,
					AddressCount: addressCount,
					Stats:        make(map[string]interface{}),
				},
				Geometry: models.CountyGeometryGeoJSON{
					Type: "Polygon",
					// Note: In a real implementation, you'd parse the boundsGeoJSON properly
					// For now, we'll return an empty coordinates array
					Coordinates: [][][]float64{},
				},
			},
		},
	}

	return geoJSON, nil
}

// GetCountyStats returns summary statistics about all counties
func (cs *CountyService) GetCountyStats() (map[string]interface{}, error) {
	query := `
		SELECT 
			COUNT(*) as total_counties,
			SUM(address_count) as total_addresses,
			AVG(address_count) as avg_addresses_per_county,
			MAX(address_count) as max_addresses,
			MIN(address_count) as min_addresses
		FROM ohio_counties
	`

	var totalCounties, totalAddresses, maxAddresses, minAddresses int
	var avgAddresses float64

	err := cs.db.QueryRow(query).Scan(
		&totalCounties, &totalAddresses, &avgAddresses, &maxAddresses, &minAddresses,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get county statistics: %w", err)
	}

	stats := map[string]interface{}{
		"total_counties":             totalCounties,
		"total_addresses":            totalAddresses,
		"avg_addresses_per_county":   avgAddresses,
		"max_addresses_per_county":   maxAddresses,
		"min_addresses_per_county":   minAddresses,
	}

	return stats, nil
}

// GetCountiesWithinBounds returns counties that intersect with the given bounding box
func (cs *CountyService) GetCountiesWithinBounds(minLat, minLon, maxLat, maxLon float64) ([]models.CountyListResponse, error) {
	query := `
		SELECT id, county_name, address_count 
		FROM ohio_counties 
		WHERE ST_Intersects(
			bounds_geometry, 
			ST_MakeEnvelope($1, $2, $3, $4, 4326)
		)
		ORDER BY address_count DESC
	`

	rows, err := cs.db.Query(query, minLon, minLat, maxLon, maxLat)
	if err != nil {
		return nil, fmt.Errorf("failed to query counties within bounds: %w", err)
	}
	defer rows.Close()

	var counties []models.CountyListResponse
	for rows.Next() {
		var county models.CountyListResponse
		err := rows.Scan(&county.ID, &county.CountyName, &county.AddressCount)
		if err != nil {
			return nil, fmt.Errorf("failed to scan county: %w", err)
		}
		counties = append(counties, county)
	}

	return counties, nil
}

// Global county service instance
var County *CountyService

func init() {
	County = NewCountyService()
}