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

	// Build the base query (will add relevance_score if needed)
	baseFields := `id, hash, house_number, street, unit, city, district, region, postcode, county,
			ST_Y(geom) as latitude, ST_X(geom) as longitude, created_at`

	// Build WHERE conditions and relevance scoring
	var conditions []string
	var args []interface{}
	var selectFields []string
	argIndex := 1
	hasRelevanceScore := false

	// Text search with relevance scoring (Google-style search)
	if params.Query != "" {
		queryWords := strings.Fields(params.Query)
		if len(queryWords) > 0 {
			// Build relevance score for ranking results
			var scoreComponents []string
			var searchConditions []string
			
			for _, word := range queryWords {
				wordPattern := "%" + word + "%"
				
				// Score: exact match in street gets highest score, partial matches get lower scores
				// Each CASE needs the SAME parameter value, so we pass the pattern once
				scoreComponents = append(scoreComponents, fmt.Sprintf(`
					CASE 
						WHEN street ILIKE $%d THEN 100
						WHEN (house_number || ' ' || street) ILIKE $%d THEN 90
						WHEN house_number ILIKE $%d THEN 80
						WHEN city ILIKE $%d THEN 60
						WHEN postcode ILIKE $%d THEN 50
						WHEN county ILIKE $%d THEN 30
						ELSE 0
					END`, argIndex, argIndex, argIndex, argIndex, argIndex, argIndex))
				
				// Search condition: word appears in ANY field (same parameter reused)
				searchConditions = append(searchConditions, fmt.Sprintf(`(
					house_number ILIKE $%d OR
					street ILIKE $%d OR
					city ILIKE $%d OR
					county ILIKE $%d OR
					postcode ILIKE $%d OR
					(house_number || ' ' || street) ILIKE $%d
				)`, argIndex, argIndex, argIndex, argIndex, argIndex, argIndex))
				
				args = append(args, wordPattern)
				argIndex++
			}
			
			// At least ONE word must match (OR logic for flexibility)
			if len(searchConditions) > 0 {
				conditions = append(conditions, "("+strings.Join(searchConditions, " OR ")+")")
			}
			
			// Add relevance score to select
			if len(scoreComponents) > 0 {
				selectFields = append(selectFields, "("+strings.Join(scoreComponents, " + ")+") as relevance_score")
				hasRelevanceScore = true
			}
		}
	}

	// County filter
	if params.County != "" {
		conditions = append(conditions, fmt.Sprintf("county ILIKE $%d", argIndex))
		args = append(args, "%"+params.County+"%")
		argIndex++
	}

	// City filter
	if params.City != "" {
		conditions = append(conditions, fmt.Sprintf("city ILIKE $%d", argIndex))
		args = append(args, "%"+params.City+"%")
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
		conditions = append(conditions, fmt.Sprintf("street ILIKE $%d", argIndex))
		args = append(args, "%"+params.Street+"%")
		argIndex++
	}

	// Proximity search
	var orderBy string
	var orderByArgs []interface{}
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
		// Order by distance - store args separately for count query
		orderBy = fmt.Sprintf(`
			ORDER BY ST_Distance(
				geom, 
				ST_SetSRID(ST_MakePoint($%d, $%d), 4326)::geography
			) ASC`, argIndex, argIndex+1)
		orderByArgs = append(orderByArgs, params.Lng, params.Lat)
		argIndex += 2
	} else if hasRelevanceScore {
		// Order by relevance score (highest first)
		orderBy = "ORDER BY relevance_score DESC, county, city, street, house_number"
	} else {
		orderBy = "ORDER BY county, city, street, house_number"
	}

	// Construct the full query
	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	// Build SELECT clause
	selectClause := baseFields
	if len(selectFields) > 0 {
		selectClause = baseFields + ", " + strings.Join(selectFields, ", ")
	}
	
	baseQuery := fmt.Sprintf("SELECT %s FROM ohio_addresses", selectClause)

	// Get total count for pagination (only use args for WHERE clause, not ORDER BY)
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM ohio_addresses %s", whereClause)
	
	var total int
	err := s.db.QueryRow(countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get total count: %w", err)
	}

	// Main query with pagination - now add ORDER BY args
	fullQueryArgs := make([]interface{}, len(args))
	copy(fullQueryArgs, args)
	fullQueryArgs = append(fullQueryArgs, orderByArgs...)
	
	fullQuery := fmt.Sprintf(`
		%s %s %s 
		LIMIT $%d OFFSET $%d
	`, baseQuery, whereClause, orderBy, argIndex, argIndex+1)
	
	fullQueryArgs = append(fullQueryArgs, params.Limit, params.Offset)

	rows, err := s.db.Query(fullQuery, fullQueryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to execute address search query: %w", err)
	}
	defer rows.Close()

	var addresses []models.OhioAddress
	for rows.Next() {
		var addr models.OhioAddress
		var relevanceScore *int // May or may not be present
		
		if hasRelevanceScore {
			err := rows.Scan(
				&addr.ID, &addr.Hash, &addr.HouseNumber, &addr.Street, &addr.Unit,
				&addr.City, &addr.District, &addr.Region, &addr.Postcode, &addr.County,
				&addr.Latitude, &addr.Longitude, &addr.CreatedAt, &relevanceScore,
			)
			if err != nil {
				return nil, 0, fmt.Errorf("failed to scan address row with score: %w", err)
			}
		} else {
			err := rows.Scan(
				&addr.ID, &addr.Hash, &addr.HouseNumber, &addr.Street, &addr.Unit,
				&addr.City, &addr.District, &addr.Region, &addr.Postcode, &addr.County,
				&addr.Latitude, &addr.Longitude, &addr.CreatedAt,
			)
			if err != nil {
				return nil, 0, fmt.Errorf("failed to scan address row: %w", err)
			}
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
	cleanQuery := strings.TrimSpace(query)
	if cleanQuery == "" {
		return []models.OhioAddress{}, nil
	}

	// Split query into words for flexible matching
	queryWords := strings.Fields(cleanQuery)
	if len(queryWords) == 0 {
		return []models.OhioAddress{}, nil
	}

	// Build word-based search with relevance scoring (same as main search)
	var args []interface{}
	var scoreComponents []string
	var searchConditions []string
	argIndex := 1
	
	for _, word := range queryWords {
		wordPattern := "%" + word + "%"
		
		// Score: exact match in street gets highest score, partial matches get lower scores
		scoreComponents = append(scoreComponents, fmt.Sprintf(`
			CASE 
				WHEN street ILIKE $%d THEN 100
				WHEN (house_number || ' ' || street) ILIKE $%d THEN 90
				WHEN house_number ILIKE $%d THEN 80
				WHEN city ILIKE $%d THEN 60
				WHEN postcode ILIKE $%d THEN 50
				WHEN county ILIKE $%d THEN 30
				ELSE 0
			END`, argIndex, argIndex, argIndex, argIndex, argIndex, argIndex))
		
		// Search condition: word appears in ANY field (OR logic)
		searchConditions = append(searchConditions, fmt.Sprintf(`(
			house_number ILIKE $%d OR
			street ILIKE $%d OR
			city ILIKE $%d OR
			county ILIKE $%d OR
			postcode ILIKE $%d OR
			(house_number || ' ' || street) ILIKE $%d
		)`, argIndex, argIndex, argIndex, argIndex, argIndex, argIndex))
		
		args = append(args, wordPattern)
		argIndex++
	}
	
	// At least ONE word must match
	whereClause := ""
	if len(searchConditions) > 0 {
		whereClause = "WHERE (" + strings.Join(searchConditions, " OR ") + ")"
	}
	
	// Build relevance score
	relevanceScore := "0"
	if len(scoreComponents) > 0 {
		relevanceScore = "(" + strings.Join(scoreComponents, " + ") + ")"
	}

	searchQuery := fmt.Sprintf(`
		SELECT 
			id, hash, house_number, street, unit, city, district, region, postcode, county,
			ST_Y(geom) as latitude, ST_X(geom) as longitude, created_at,
			%s as relevance_score
		FROM ohio_addresses
		%s
		ORDER BY relevance_score DESC, county, city, street, house_number
		LIMIT $%d
	`, relevanceScore, whereClause, argIndex)

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
		var relevanceScoreVal int
		var unit, district sql.NullString

		err := rows.Scan(
			&addr.ID, &addr.Hash, &addr.HouseNumber, &addr.Street, &unit,
			&addr.City, &district, &addr.Region, &addr.Postcode, &addr.County,
			&addr.Latitude, &addr.Longitude, &addr.CreatedAt, &relevanceScoreVal,
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