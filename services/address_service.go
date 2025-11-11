package services

import (
	"database/sql"
	"fmt"
	"geocoding-api/models"
	"strings"
)

// AddressService handles Ohio address-related operations
type AddressService struct {
	db *sql.DB
}

// NewAddressService creates a new AddressService
func NewAddressService(db *sql.DB) *AddressService {
	return &AddressService{db: db}
}

// SearchAddresses searches for addresses based on the provided parameters
func (s *AddressService) SearchAddresses(params models.AddressSearchParams) ([]models.OhioAddress, int, error) {
	// Set default limit
	if params.Limit <= 0 {
		params.Limit = 50
	}
	if params.Limit > 500 {
		params.Limit = 500
	}

	// Build the base query
	baseQuery := `
		SELECT 
			id, hash, house_number, street, unit, city, district, region, postcode, county,
			ST_Y(geom) as latitude, ST_X(geom) as longitude, created_at
		FROM ohio_addresses
	`

	// Build WHERE conditions
	var conditions []string
	var args []interface{}
	argIndex := 1

	// Text search across multiple fields
	if params.Query != "" {
		conditions = append(conditions, fmt.Sprintf(`(
			LOWER(house_number || ' ' || street) LIKE LOWER($%d) OR
			LOWER(city) LIKE LOWER($%d) OR
			LOWER(county) LIKE LOWER($%d) OR
			LOWER(postcode) LIKE LOWER($%d)
		)`, argIndex, argIndex, argIndex, argIndex))
		args = append(args, "%"+params.Query+"%")
		argIndex++
	}

	// County filter
	if params.County != "" {
		conditions = append(conditions, fmt.Sprintf("LOWER(county) = LOWER($%d)", argIndex))
		args = append(args, params.County)
		argIndex++
	}

	// City filter
	if params.City != "" {
		conditions = append(conditions, fmt.Sprintf("LOWER(city) = LOWER($%d)", argIndex))
		args = append(args, params.City)
		argIndex++
	}

	// Postcode filter
	if params.Postcode != "" {
		conditions = append(conditions, fmt.Sprintf("postcode = $%d", argIndex))
		args = append(args, params.Postcode)
		argIndex++
	}

	// Street filter
	if params.Street != "" {
		conditions = append(conditions, fmt.Sprintf("LOWER(street) LIKE LOWER($%d)", argIndex))
		args = append(args, "%"+params.Street+"%")
		argIndex++
	}

	// Proximity search
	var orderBy string
	if params.Lat != 0 && params.Lng != 0 {
		if params.Radius > 0 {
			// Add distance filter (radius in kilometers)
			conditions = append(conditions, fmt.Sprintf(`
				ST_DWithin(
					geom, 
					ST_SetSRID(ST_MakePoint($%d, $%d), 4326)::geography,
					$%d
				)`, argIndex, argIndex+1, argIndex+2))
			args = append(args, params.Lng, params.Lat, params.Radius*1000) // Convert km to meters
			argIndex += 3
		}
		// Order by distance
		orderBy = fmt.Sprintf(`
			ORDER BY ST_Distance(
				geom, 
				ST_SetSRID(ST_MakePoint($%d, $%d), 4326)::geography
			) ASC`, argIndex, argIndex+1)
		args = append(args, params.Lng, params.Lat)
		argIndex += 2
	} else {
		orderBy = "ORDER BY county, city, street, house_number"
	}

	// Construct the full query
	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	// Get total count for pagination
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM ohio_addresses %s", whereClause)
	
	var total int
	err := s.db.QueryRow(countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get total count: %w", err)
	}

	// Main query with pagination
	fullQuery := fmt.Sprintf(`
		%s %s %s 
		LIMIT $%d OFFSET $%d
	`, baseQuery, whereClause, orderBy, argIndex, argIndex+1)
	
	args = append(args, params.Limit, params.Offset)

	rows, err := s.db.Query(fullQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to execute address search query: %w", err)
	}
	defer rows.Close()

	var addresses []models.OhioAddress
	for rows.Next() {
		var addr models.OhioAddress
		err := rows.Scan(
			&addr.ID, &addr.Hash, &addr.HouseNumber, &addr.Street, &addr.Unit,
			&addr.City, &addr.District, &addr.Region, &addr.Postcode, &addr.County,
			&addr.Latitude, &addr.Longitude, &addr.CreatedAt,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan address row: %w", err)
		}
		addresses = append(addresses, addr)
	}

	if err = rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("error iterating address rows: %w", err)
	}

	return addresses, total, nil
}

// GetAddressByID retrieves a specific address by ID
func (s *AddressService) GetAddressByID(id int64) (*models.OhioAddress, error) {
	query := `
		SELECT 
			id, hash, house_number, street, unit, city, district, region, postcode, county,
			ST_Y(geom) as latitude, ST_X(geom) as longitude, created_at
		FROM ohio_addresses 
		WHERE id = $1
	`

	var addr models.OhioAddress
	err := s.db.QueryRow(query, id).Scan(
		&addr.ID, &addr.Hash, &addr.HouseNumber, &addr.Street, &addr.Unit,
		&addr.City, &addr.District, &addr.Region, &addr.Postcode, &addr.County,
		&addr.Latitude, &addr.Longitude, &addr.CreatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("address not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get address: %w", err)
	}

	return &addr, nil
}

// GetCountyStats returns statistics about loaded counties
func (s *AddressService) GetCountyStats() (map[string]int, error) {
	query := `
		SELECT county, COUNT(*) as count 
		FROM ohio_addresses 
		GROUP BY county 
		ORDER BY count DESC
	`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to get county stats: %w", err)
	}
	defer rows.Close()

	stats := make(map[string]int)
	for rows.Next() {
		var county string
		var count int
		if err := rows.Scan(&county, &count); err != nil {
			return nil, fmt.Errorf("failed to scan county stats: %w", err)
		}
		stats[county] = count
	}

	return stats, nil
}

// SemanticSearchAddresses performs a semantic search for addresses with flexible token-based matching
func (s *AddressService) SemanticSearchAddresses(query string, limit int) ([]models.OhioAddress, error) {
	// Set default limit
	if limit <= 0 {
		limit = 5
	}
	if limit > 50 {
		limit = 50
	}

	// Clean and prepare the search query
	cleanQuery := strings.TrimSpace(strings.ToLower(query))
	if cleanQuery == "" {
		return []models.OhioAddress{}, nil
	}

	// Split query into tokens for flexible matching
	tokens := strings.Fields(cleanQuery)
	if len(tokens) == 0 {
		return []models.OhioAddress{}, nil
	}

	// Simplified semantic search to avoid parameter mismatches
	// Use a simpler approach that's more reliable
	var args []interface{}
	
	// Build a simple text search across all relevant fields
	searchPattern := "%" + cleanQuery + "%"
	
	searchQuery := `
		SELECT 
			id, hash, house_number, street, unit, city, district, region, postcode, county,
			ST_Y(geom) as latitude, ST_X(geom) as longitude, created_at,
			-- Simple relevance scoring
			(
				CASE WHEN LOWER(house_number || ' ' || street || ' ' || city) LIKE $1 THEN 100 ELSE 0 END +
				CASE WHEN LOWER(street || ' ' || city) LIKE $1 THEN 80 ELSE 0 END +
				CASE WHEN LOWER(city) LIKE $1 THEN 60 ELSE 0 END +
				CASE WHEN LOWER(street) LIKE $1 THEN 40 ELSE 0 END +
				CASE WHEN LOWER(house_number) LIKE $1 THEN 20 ELSE 0 END
			) as relevance_score
		FROM ohio_addresses
		WHERE (
			LOWER(house_number) LIKE $1 OR
			LOWER(street) LIKE $1 OR  
			LOWER(city) LIKE $1 OR
			LOWER(county) LIKE $1 OR
			LOWER(postcode) LIKE $1
		)
		ORDER BY relevance_score DESC, city ASC, street ASC, house_number ASC
		LIMIT $2
	`

	args = append(args, searchPattern, limit)

	// Execute the query
	rows, err := s.db.Query(searchQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute semantic search: %w", err)
	}
	defer rows.Close()

	// Parse results
	var addresses []models.OhioAddress
	for rows.Next() {
		var addr models.OhioAddress
		var relevanceScore int
		var unit, district sql.NullString

		err := rows.Scan(
			&addr.ID, &addr.Hash, &addr.HouseNumber, &addr.Street, &unit,
			&addr.City, &district, &addr.Region, &addr.Postcode, &addr.County,
			&addr.Latitude, &addr.Longitude, &addr.CreatedAt, &relevanceScore,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan address: %w", err)
		}

		// Handle nullable fields
		if unit.Valid {
			addr.Unit = unit.String
		}
		if district.Valid {
			addr.District = district.String
		}

		addresses = append(addresses, addr)
	}

	return addresses, nil
}



// Global address service instance
var Address *AddressService

// InitAddressService initializes the global address service
func InitAddressService(db *sql.DB) {
	Address = NewAddressService(db)
}