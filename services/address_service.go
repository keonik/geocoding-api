package services

import (
	"database/sql"
	"fmt"
	"geocoding-api/models"
	"geocoding-api/utils"
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
	baseFields := `id, hash, house_number, street, unit, city, district, region, postcode, county, full_address,
			ST_Y(geom) as latitude, ST_X(geom) as longitude, created_at`

	// Build WHERE conditions and relevance scoring
	var conditions []string
	var args []interface{}
	var selectFields []string
	argIndex := 1
	hasRelevanceScore := false

	// Text search with relevance scoring (Google-style search)
	if params.Query != "" {
		// Strip unit designators (#F, Apt 2B, Suite 100, etc.) to avoid
		// search terms that won't match any database fields
		params.Query = utils.StripUnitDesignator(params.Query)
		queryWords := strings.Fields(params.Query)
		if len(queryWords) > 0 {
			// Build relevance score for ranking results
			var scoreComponents []string
			var searchConditions []string
			
			for _, word := range queryWords {
				wordPattern := "%" + word + "%"
				
				// Score: full_address match gets highest priority, then specific fields
				scoreComponents = append(scoreComponents, fmt.Sprintf(`
					CASE 
						WHEN full_address ILIKE $%d THEN 150
						WHEN street ILIKE $%d THEN 100
						WHEN (house_number || ' ' || street) ILIKE $%d THEN 90
						WHEN house_number ILIKE $%d THEN 80
						WHEN city ILIKE $%d THEN 60
						WHEN postcode ILIKE $%d THEN 50
						WHEN county ILIKE $%d THEN 30
						ELSE 0
					END`, argIndex, argIndex, argIndex, argIndex, argIndex, argIndex, argIndex))
				
				// Search condition: word must appear in SOME field (each word required via AND)
				searchConditions = append(searchConditions, fmt.Sprintf(`(
					full_address ILIKE $%d OR
					house_number ILIKE $%d OR
					street ILIKE $%d OR
					city ILIKE $%d OR
					county ILIKE $%d OR
					postcode ILIKE $%d
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
				&addr.City, &addr.District, &addr.Region, &addr.Postcode, &addr.County, &addr.FullAddress,
				&addr.Latitude, &addr.Longitude, &addr.CreatedAt, &relevanceScore,
			)
			if err != nil {
				return nil, 0, fmt.Errorf("failed to scan address row with score: %w", err)
			}
		} else {
			err := rows.Scan(
				&addr.ID, &addr.Hash, &addr.HouseNumber, &addr.Street, &addr.Unit,
				&addr.City, &addr.District, &addr.Region, &addr.Postcode, &addr.County, &addr.FullAddress,
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
			id, hash, house_number, street, unit, city, district, region, postcode, county, full_address,
			ST_Y(geom) as latitude, ST_X(geom) as longitude, created_at
		FROM ohio_addresses 
		WHERE id = $1
	`

	var addr models.OhioAddress
	err := s.db.QueryRow(query, id).Scan(
		&addr.ID, &addr.Hash, &addr.HouseNumber, &addr.Street, &addr.Unit,
		&addr.City, &addr.District, &addr.Region, &addr.Postcode, &addr.County, &addr.FullAddress,
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

// AddressSearchResult contains search results along with metadata about the search
type AddressSearchResult struct {
	Addresses       []models.OhioAddress
	ExactCount      int    // Number of exact matches
	FallbackCount   int    // Number of fallback (street-only) matches
	FallbackQuery   string // The query used for fallback (empty if no fallback)
	OriginalQuery   string
}

// FullTextSearchAddresses performs a simple full-text search on the full_address column
// Returns exact matches first, followed by street-level matches (fallback) with lower priority
func (s *AddressService) FullTextSearchAddresses(query string, limit int) (*AddressSearchResult, error) {
	result := &AddressSearchResult{
		OriginalQuery: query,
	}

	// Set default limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}

	// Clean query
	query = strings.TrimSpace(query)
	if query == "" {
		result.Addresses = []models.OhioAddress{}
		return result, nil
	}

	// Strip unit designators (#F, Apt 2B, Suite 100, etc.) since the database
	// stores addresses without these, allowing fallback to the base address
	query = utils.StripUnitDesignator(query)

	// Get the street-only version of the query for fallback
	fallbackQuery := extractStreetFromQuery(query)
	hasFallback := fallbackQuery != "" && fallbackQuery != query

	// If there's no fallback possible (query has no house number), just do a simple search
	if !hasFallback {
		addresses, err := s.searchAddressesWithVariants(query, limit)
		if err != nil {
			return nil, err
		}
		result.Addresses = addresses
		result.ExactCount = len(addresses)
		return result, nil
	}

	// Build a combined query that returns exact matches first, then street matches
	// This uses a single query with UNION to get both result sets in priority order
	addresses, exactCount, fallbackCount, err := s.searchWithFallback(query, fallbackQuery, limit)
	if err != nil {
		return nil, err
	}

	result.Addresses = addresses
	result.ExactCount = exactCount
	result.FallbackCount = fallbackCount
	if fallbackCount > 0 {
		result.FallbackQuery = fallbackQuery
	}

	return result, nil
}

// searchWithFallback performs a search that returns exact matches first, then street-level fallback matches
func (s *AddressService) searchWithFallback(exactQuery, fallbackQuery string, limit int) ([]models.OhioAddress, int, int, error) {
	// Get variants for both queries
	exactVariants := utils.GetAddressQueryVariants(exactQuery)
	fallbackVariants := utils.GetAddressQueryVariants(fallbackQuery)

	// Build exact match conditions
	var exactConditions []string
	var args []interface{}
	argNum := 1

	for _, variant := range exactVariants {
		pattern := "%" + variant + "%"
		exactConditions = append(exactConditions, fmt.Sprintf("full_address ILIKE $%d", argNum))
		args = append(args, pattern)
		argNum++
	}

	// Build fallback match conditions (but exclude exact matches)
	var fallbackConditions []string
	for _, variant := range fallbackVariants {
		pattern := "%" + variant + "%"
		fallbackConditions = append(fallbackConditions, fmt.Sprintf("full_address ILIKE $%d", argNum))
		args = append(args, pattern)
		argNum++
	}

	// Build the combined query using UNION ALL with priority ordering
	// Priority 1 = exact matches, Priority 2 = fallback matches
	searchQuery := fmt.Sprintf(`
		WITH exact_matches AS (
			SELECT 
				id, hash, house_number, street, unit, city, district, region, postcode, county, full_address,
				ST_Y(geom) as latitude, ST_X(geom) as longitude, created_at,
				1 as priority
			FROM ohio_addresses
			WHERE %s
		),
		fallback_matches AS (
			SELECT 
				id, hash, house_number, street, unit, city, district, region, postcode, county, full_address,
				ST_Y(geom) as latitude, ST_X(geom) as longitude, created_at,
				2 as priority
			FROM ohio_addresses
			WHERE (%s)
			AND id NOT IN (SELECT id FROM exact_matches)
		),
		combined AS (
			SELECT * FROM exact_matches
			UNION ALL
			SELECT * FROM fallback_matches
		)
		SELECT id, hash, house_number, street, unit, city, district, region, postcode, county, full_address,
			   latitude, longitude, created_at, priority
		FROM combined
		ORDER BY priority, full_address
		LIMIT $%d
	`, strings.Join(exactConditions, " OR "), strings.Join(fallbackConditions, " OR "), argNum)

	args = append(args, limit)

	rows, err := s.db.Query(searchQuery, args...)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("failed to execute search with fallback: %w", err)
	}
	defer rows.Close()

	var addresses []models.OhioAddress
	exactCount := 0
	fallbackCount := 0

	for rows.Next() {
		var addr models.OhioAddress
		var unit, district sql.NullString
		var priority int

		err := rows.Scan(
			&addr.ID, &addr.Hash, &addr.HouseNumber, &addr.Street, &unit,
			&addr.City, &district, &addr.Region, &addr.Postcode, &addr.County, &addr.FullAddress,
			&addr.Latitude, &addr.Longitude, &addr.CreatedAt, &priority,
		)
		if err != nil {
			return nil, 0, 0, fmt.Errorf("failed to scan address: %w", err)
		}

		// Handle nullable fields
		if unit.Valid {
			addr.Unit = unit.String
		}
		if district.Valid {
			addr.District = district.String
		}

		addresses = append(addresses, addr)

		// Count by priority
		if priority == 1 {
			exactCount++
		} else {
			fallbackCount++
		}
	}

	if err = rows.Err(); err != nil {
		return nil, 0, 0, fmt.Errorf("error iterating address rows: %w", err)
	}

	return addresses, exactCount, fallbackCount, nil
}

// searchAddressesWithVariants performs the actual search with abbreviation variants
func (s *AddressService) searchAddressesWithVariants(query string, limit int) ([]models.OhioAddress, error) {
	// Get all variants of the query (handles both abbreviations and full forms)
	// This allows "dr" to match "drive" and "drive" to match "dr"
	queryVariants := utils.GetAddressQueryVariants(query)
	
	// Build OR conditions for all variants
	var conditions []string
	var args []interface{}
	argNum := 1
	
	for _, variant := range queryVariants {
		pattern := "%" + variant + "%"
		conditions = append(conditions, fmt.Sprintf("full_address ILIKE $%d", argNum))
		args = append(args, pattern)
		argNum++
	}

	// Search using the full_address column with trigram index
	searchQuery := fmt.Sprintf(`
		SELECT 
			id, hash, house_number, street, unit, city, district, region, postcode, county, full_address,
			ST_Y(geom) as latitude, ST_X(geom) as longitude, created_at
		FROM ohio_addresses
		WHERE %s
		ORDER BY 
			CASE 
				WHEN full_address ILIKE $%d THEN 1  -- Exact match to original query
				ELSE 2
			END,
			full_address
		LIMIT $%d
	`, strings.Join(conditions, " OR "), argNum, argNum+1)

	// Add exact pattern and limit
	exactPattern := "%" + query + "%"
	args = append(args, exactPattern, limit)

	rows, err := s.db.Query(searchQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute full-text search: %w", err)
	}
	defer rows.Close()

	var addresses []models.OhioAddress
	for rows.Next() {
		var addr models.OhioAddress
		var unit, district sql.NullString

		err := rows.Scan(
			&addr.ID, &addr.Hash, &addr.HouseNumber, &addr.Street, &unit,
			&addr.City, &district, &addr.Region, &addr.Postcode, &addr.County, &addr.FullAddress,
			&addr.Latitude, &addr.Longitude, &addr.CreatedAt,
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

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating address rows: %w", err)
	}

	return addresses, nil
}

// extractStreetFromQuery removes the house number from an address query
// to enable street-only fallback search.
// Example: "8 Prestige Plaza, Miamisburg OH" -> "Prestige Plaza, Miamisburg OH"
// Example: "123 Main St" -> "Main St"
func extractStreetFromQuery(query string) string {
	query = strings.TrimSpace(query)
	words := strings.Fields(query)
	
	if len(words) < 2 {
		return query
	}
	
	// Check if the first word looks like a house number
	firstWord := words[0]
	
	// House numbers are typically:
	// - Pure digits: "123"
	// - Digits with letter suffix: "123A", "456B"
	// - Digit ranges: "100-102"
	isHouseNumber := false
	
	// Check if it starts with a digit
	if len(firstWord) > 0 && firstWord[0] >= '0' && firstWord[0] <= '9' {
		isHouseNumber = true
		// Verify it's mostly numeric (allow for suffixes like "A", "B" or ranges like "100-102")
		digitCount := 0
		for _, c := range firstWord {
			if c >= '0' && c <= '9' {
				digitCount++
			}
		}
		// At least half should be digits
		if digitCount < len(firstWord)/2 {
			isHouseNumber = false
		}
	}
	
	if isHouseNumber {
		// Return everything after the house number
		return strings.Join(words[1:], " ")
	}
	
	return query
}

// CreateAddress inserts a new address into the database
func (s *AddressService) CreateAddress(address *models.OhioAddress) (int, error) {
	query := `
		INSERT INTO ohio_addresses (
			hash, house_number, street, unit, city, district, region, postcode, county, geom
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, ST_SetSRID(ST_MakePoint($10, $11), 4326)
		)
		RETURNING id
	`

	// Generate hash for deduplication
	hash := fmt.Sprintf("%s|%s|%s|%s|%s",
		address.HouseNumber, address.Street, address.Unit, address.City, address.Postcode)

	var id int
	err := s.db.QueryRow(
		query,
		hash,
		address.HouseNumber,
		address.Street,
		address.Unit,
		address.City,
		address.District,
		address.Region,
		address.Postcode,
		address.County,
		address.Longitude,
		address.Latitude,
	).Scan(&id)

	return id, err
}

// Global address service instance
var Address *AddressService

// InitAddressService initializes the global address service
func InitAddressService(db *sql.DB) {
	Address = NewAddressService(db)
}

// GetDB returns the database connection from the address service
func GetDB() *sql.DB {
	if Address != nil {
		return Address.db
	}
	return nil
}