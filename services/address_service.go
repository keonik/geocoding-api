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

	// Build flexible semantic search query that handles tokens in any order
	var whereConditions []string
	var args []interface{}
	argIndex := 1

	// Add individual token matching conditions
	for _, token := range tokens {
		tokenPattern := "%" + token + "%"
		whereConditions = append(whereConditions, fmt.Sprintf(`(
			LOWER(house_number) LIKE $%d OR
			LOWER(street) LIKE $%d OR  
			LOWER(city) LIKE $%d OR
			LOWER(county) LIKE $%d OR
			LOWER(postcode) LIKE $%d
		)`, argIndex, argIndex, argIndex, argIndex, argIndex))
		args = append(args, tokenPattern)
		argIndex++
	}

	// Build the main search query with advanced relevance scoring
	searchQuery := fmt.Sprintf(`
		SELECT 
			id, hash, house_number, street, unit, city, district, region, postcode, county,
			ST_Y(geom) as latitude, ST_X(geom) as longitude, created_at,
			-- Calculate advanced relevance score based on token matches
			(
				-- Perfect full query match (any order)
				CASE WHEN LOWER(house_number || ' ' || street || ' ' || city) LIKE $%d THEN 1000 ELSE 0 END +
				CASE WHEN LOWER(street || ' ' || city) LIKE $%d THEN 900 ELSE 0 END +
				CASE WHEN LOWER(house_number || ' ' || street) LIKE $%d THEN 850 ELSE 0 END +
				CASE WHEN LOWER(city || ' ' || street) LIKE $%d THEN 800 ELSE 0 END +
				
				-- Individual field exact matches
				CASE WHEN LOWER(house_number) = $%d THEN 200 ELSE 0 END +
				CASE WHEN LOWER(street) LIKE $%d THEN 150 ELSE 0 END +
				CASE WHEN LOWER(city) LIKE $%d THEN 100 ELSE 0 END +
				CASE WHEN LOWER(county) LIKE $%d THEN 50 ELSE 0 END +
				CASE WHEN LOWER(postcode) LIKE $%d THEN 75 ELSE 0 END +
				
				-- Token matching bonus (more tokens matched = higher score)
				%s
			) as relevance_score
		FROM ohio_addresses
		WHERE (%s)
		ORDER BY relevance_score DESC, city ASC, street ASC, house_number ASC
		LIMIT $%d
	`, 
		argIndex, argIndex, argIndex, argIndex,     // Full query patterns
		argIndex, argIndex, argIndex, argIndex, argIndex, // Individual field patterns
		buildTokenBonusSQL(len(tokens), argIndex), // Token bonus calculation
		strings.Join(whereConditions, " AND "),     // WHERE conditions
		argIndex+len(tokens)) // LIMIT parameter

	// Add the full query pattern arguments
	fullQueryPattern := "%" + cleanQuery + "%"
	for i := 0; i < 9; i++ { // 9 times for the relevance scoring patterns
		args = append(args, fullQueryPattern)
	}
	argIndex += 9

	// Add token arguments for bonus scoring
	for _, token := range tokens {
		args = append(args, "%"+token+"%")
	}

	// Add limit
	args = append(args, limit)

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

// buildTokenBonusSQL generates SQL for token-based bonus scoring
func buildTokenBonusSQL(tokenCount int, startIndex int) string {
	var bonuses []string
	for i := 0; i < tokenCount; i++ {
		argIndex := startIndex + i
		bonus := fmt.Sprintf(`(
			CASE WHEN LOWER(house_number) LIKE $%d THEN 20 ELSE 0 END +
			CASE WHEN LOWER(street) LIKE $%d THEN 30 ELSE 0 END +
			CASE WHEN LOWER(city) LIKE $%d THEN 25 ELSE 0 END +
			CASE WHEN LOWER(county) LIKE $%d THEN 15 ELSE 0 END +
			CASE WHEN LOWER(postcode) LIKE $%d THEN 10 ELSE 0 END
		)`, argIndex, argIndex, argIndex, argIndex, argIndex)
		bonuses = append(bonuses, bonus)
	}
	return strings.Join(bonuses, " + ")
}

// Global address service instance
var Address *AddressService

// InitAddressService initializes the global address service
func InitAddressService(db *sql.DB) {
	Address = NewAddressService(db)
}