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

// querier is the subset of *sql.DB and *sql.Tx that the search path needs, so
// the same builder can run against a plain connection or inside a transaction.
type querier interface {
	Query(query string, args ...interface{}) (*sql.Rows, error)
	QueryRow(query string, args ...interface{}) *sql.Row
}

// fuzzyWordSimilarityThreshold is the pg_trgm word_similarity cutoff for the
// fallback search. It is set explicitly rather than left to the server default
// so behaviour does not drift with server configuration.
//
// 0.6 is the knee of the curve. Measured against 5.9M Ohio addresses, scoring
// each typo shape of "Barendt":
//
//	truncation "barend"       0.857    recovered
//	doubled    "barendtt"     0.778    recovered
//	substitution (end)        0.750    recovered
//	fragment   "arendt"       0.714    recovered
//	insertion  "barrendt"     0.700    recovered
//	deletion   "barndt"       0.500    missed
//	substitution (mid)        0.455    missed
//	transposition "barnedt"   0.375    missed
//
// Lowering the threshold to reach the bottom three does not work: "barnedt"
// scores 0.375 against Barendt but 0.625 against the unrelated "Barnes Run",
// so a cutoff low enough to admit the target ranks the wrong street above it.
// Transpositions need edit distance, not trigrams - see the note on
// SearchAddresses.
const fuzzyWordSimilarityThreshold = 0.6

// SearchAddresses searches for addresses based on the provided parameters.
//
// Two passes. The first is an exact word-prefix full-text match, which serves
// every well-formed query in under 2ms. If it returns nothing and the caller
// supplied query text, a trigram word-similarity pass runs as a fallback so
// misspellings and mid-word fragments degrade into near matches instead of an
// empty list. The fallback costs ~7-12ms and only ever runs on a miss.
//
// Known gap: character transpositions ("barnedt" for "Barendt") are not
// recovered by either pass, for the reason documented on
// fuzzyWordSimilarityThreshold. Closing that needs a Levenshtein pass over a
// candidate set - worth doing only if it shows up in real query logs.
func (s *AddressService) SearchAddresses(params models.AddressSearchParams) ([]models.OhioAddress, int, error) {
	addresses, total, err := s.searchAddresses(s.db, params, false)
	if err != nil || total > 0 || params.Query == "" {
		return addresses, total, err
	}

	// Fallback runs in a transaction so SET LOCAL scopes the threshold to
	// these two statements and cannot leak onto a pooled connection.
	tx, err := s.db.Begin()
	if err != nil {
		return nil, 0, fmt.Errorf("failed to begin fuzzy search transaction: %w", err)
	}
	defer tx.Rollback() // read-only; Rollback after Commit is a no-op

	if _, err := tx.Exec(fmt.Sprintf("SET LOCAL pg_trgm.word_similarity_threshold = %g", fuzzyWordSimilarityThreshold)); err != nil {
		return nil, 0, fmt.Errorf("failed to set word similarity threshold: %w", err)
	}

	addresses, total, err = s.searchAddresses(tx, params, true)
	if err != nil {
		return nil, 0, err
	}
	if err := tx.Commit(); err != nil {
		return nil, 0, fmt.Errorf("failed to commit fuzzy search transaction: %w", err)
	}
	return addresses, total, nil
}

// searchAddresses builds and runs the search. When fuzzy is false the text
// predicate is an indexed word-prefix tsquery; when true it is a per-word
// trigram similarity match against the full_address trigram index.
func (s *AddressService) searchAddresses(q querier, params models.AddressSearchParams, fuzzy bool) ([]models.OhioAddress, int, error) {
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

	// queryWords outlives this block. The relevance score it feeds lives in the
	// SELECT clause, and its parameters have to be numbered after every WHERE
	// parameter, so it is built further down rather than here.
	var queryWords []string

	// Text search with relevance scoring (Google-style search)
	//
	// The predicate is a single word-prefix full-text match against the `fts`
	// GIN index (migration 18). It replaces an ILIKE '%x%' OR-chain across six
	// columns: because county and postcode have only btree indexes, a leading
	// wildcard on them was unindexable, so the planner sequential-scanned all
	// ~6M rows on every search (~2.7s measured). This runs in <2ms.
	//
	// Note: prefix matching is anchored at word starts. "bare" matches
	// "Barendt Road", but a typo or a mid-word fragment will not; those fall
	// through to the trigram pass driven from SearchAddresses.
	if params.Query != "" {
		// Strip unit designators (#F, Apt 2B, Suite 100, etc.) to avoid
		// search terms that won't match any database fields
		params.Query = utils.StripUnitDesignator(params.Query)

		// Single-character fragments yield no usable prefix term, so drop them
		// rather than let them widen the result set.
		for _, w := range strings.Fields(params.Query) {
			if len(sanitizeTSTerm(w)) >= 2 {
				queryWords = append(queryWords, w)
			}
		}

		if len(queryWords) > 0 {
			// Every word must match (AND logic for precision).
			if fuzzy {
				// One trigram condition per word. The indexable form is
				// `pattern <% column`, which the planner rewrites to
				// `column %> pattern` against the full_address GIN trigram
				// index (idx_ohio_addresses_full_address_trgm, migration 15).
				for _, word := range queryWords {
					conditions = append(conditions, fmt.Sprintf("$%d <%% full_address", argIndex))
					args = append(args, word)
					argIndex++
				}
			} else {
				conditions = append(conditions, fmt.Sprintf("fts @@ to_tsquery('simple', $%d)", argIndex))
				args = append(args, buildPrefixTSQuery(queryWords))
				argIndex++
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

	// Proximity filter. This is the last thing to contribute a WHERE parameter.
	if params.Lat != 0 && params.Lng != 0 && params.Radius > 0 {
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

	// Every WHERE parameter has now been assigned, so $1..$whereArgCount are
	// exactly the ones the count query below references. Anything numbered past
	// this point belongs to SELECT or ORDER BY, and handing those to the count
	// query is what broke it in production: Postgres infers a parameter's type
	// from where it is used, so an argument the statement never mentions fails
	// to parse with "could not determine data type of parameter $1".
	whereArgCount := len(args)

	// Build relevance score for ranking results. These CASE arms are evaluated
	// only for rows the index already matched, so the ILIKEs here cost nothing
	// like they did in the WHERE clause.
	if len(queryWords) > 0 {
		var scoreComponents []string

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

			args = append(args, wordPattern)
			argIndex++
		}

		selectFields = append(selectFields, "("+strings.Join(scoreComponents, " + ")+") as relevance_score")
		hasRelevanceScore = true
	}

	// Ordering. Distance ordering takes lat/lng as fresh parameters, numbered
	// after the SELECT ones so placeholder numbers keep matching positions in
	// fullQueryArgs.
	var orderBy string
	var orderByArgs []interface{}
	if params.Lat != 0 && params.Lng != 0 {
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
	err := q.QueryRow(countQuery, args[:whereArgCount]...).Scan(&total)
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

	rows, err := q.Query(fullQuery, fullQueryArgs...)
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
	ExactCount      int                  // Number of exact matches
	FallbackCount   int                  // Number of fallback (street-only) matches
	FallbackQuery   string               // The query used for fallback (empty if no fallback)
	OriginalQuery   string
	ParsedQuery     *utils.ParsedAddress // Parsed address components (nil if not parsed)
	SearchMethod    string               // "component" or "fulltext"
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

	// Try smart component-based search first: parse the query into parts
	// (house number, street, city, state, zip) and match against individual fields.
	// This handles cases where the user's formatting differs from the database.
	parsed := utils.ParseAddressQuery(query)
	result.ParsedQuery = parsed

	if parsed.Street != "" || parsed.City != "" || parsed.Zip != "" {
		componentResult, err := s.searchByComponents(parsed, limit)
		if err == nil && componentResult != nil && len(componentResult.Addresses) > 0 {
			result.Addresses = componentResult.Addresses
			result.ExactCount = componentResult.ExactCount
			result.FallbackCount = componentResult.NearbyCount
			result.SearchMethod = "component"
			if componentResult.NearbyCount > 0 {
				result.FallbackQuery = "nearby addresses (street/city match)"
			}
			return result, nil
		}
	}

	// Component parsing only splits cleanly when the query is comma-delimited.
	// ParseAddressQuery("7057 barendt toledo") yields street="barendt toledo"
	// with no city, so every tier matches nothing; ParseAddressQuery("bare")
	// yields city="bare", which searches city names rather than streets. Both
	// are ordinary autocomplete input. Try an indexed word-prefix match before
	// dropping to the substring path, which requires the words to be adjacent
	// in full_address and so misses them too.
	if addresses, err := s.searchAddressesByPrefix(query, limit); err == nil && len(addresses) > 0 {
		result.Addresses = addresses
		result.ExactCount = len(addresses)
		result.SearchMethod = "prefix"
		return result, nil
	}

	// Fall back to full_address ILIKE search if component search found nothing
	result.SearchMethod = "fulltext"

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

// componentSearchResult holds addresses with exact vs nearby counts from tiered search.
type componentSearchResult struct {
	Addresses    []models.OhioAddress
	ExactCount   int // Tiers that matched the house number (exact address)
	NearbyCount  int // Tiers that dropped the house number (same street/city)
	BestTier     int // The most specific tier that returned results
}

// searchByComponents searches using parsed address components against individual fields.
// It builds a tiered CTE query that tries the most specific match first and progressively
// relaxes conditions to find nearby results.
//
// Tiers with house number matching are "exact"; tiers without are "nearby" fallbacks.
func (s *AddressService) searchByComponents(parsed *utils.ParsedAddress, limit int) (*componentSearchResult, error) {
	var args []interface{}
	argNum := 1

	// Build street ILIKE conditions using abbreviation variants
	hasStreet := parsed.Street != ""
	streetClause := ""
	if hasStreet {
		streetVariants := utils.GetAddressQueryVariants(parsed.Street)
		var streetConditions []string
		for _, variant := range streetVariants {
			streetConditions = append(streetConditions, fmt.Sprintf("street ILIKE $%d", argNum))
			args = append(args, "%"+variant+"%")
			argNum++
		}
		streetClause = "(" + strings.Join(streetConditions, " OR ") + ")"
	}

	// Prepare optional component placeholders
	houseArg := 0
	if parsed.HouseNumber != "" {
		houseArg = argNum
		args = append(args, parsed.HouseNumber)
		argNum++
	}

	cityArg := 0
	if parsed.City != "" {
		cityArg = argNum
		args = append(args, parsed.City)
		argNum++
	}

	zipArg := 0
	if parsed.Zip != "" {
		zipArg = argNum
		args = append(args, parsed.Zip)
		argNum++
	}

	// Build tiers dynamically based on which components we have.
	// Each tier is more relaxed than the previous one.
	selectFields := `id, hash, house_number, street, unit, city, district, region, postcode, county, full_address,
		ST_Y(geom) as latitude, ST_X(geom) as longitude, created_at`

	var tierCTEs []string
	var tierSelects []string
	var exclusions []string
	tierNum := 0
	// Track which tiers include the house number (exact) vs not (nearby)
	exactTiers := make(map[int]bool)

	addTier := func(whereClause string, isExact bool) {
		tierNum++
		tierName := fmt.Sprintf("tier%d", tierNum)
		exclusionClause := ""
		if len(exclusions) > 0 {
			exclusionClause = " AND " + strings.Join(exclusions, " AND ")
		}
		tierCTEs = append(tierCTEs, fmt.Sprintf(`%s AS (
			SELECT %s, %d as tier FROM ohio_addresses
			WHERE %s%s
			LIMIT %d
		)`, tierName, selectFields, tierNum, whereClause, exclusionClause, limit))
		tierSelects = append(tierSelects, fmt.Sprintf("SELECT * FROM %s", tierName))
		exclusions = append(exclusions, fmt.Sprintf("id NOT IN (SELECT id FROM %s)", tierName))
		if isExact {
			exactTiers[tierNum] = true
		}
	}

	// Location anchor: always constrain to the provided city/zip/both.
	// What relaxes is the street-level detail, not the location.
	locationClause := ""
	switch {
	case cityArg > 0 && zipArg > 0:
		locationClause = fmt.Sprintf("city ILIKE $%d AND postcode = $%d", cityArg, zipArg)
	case zipArg > 0:
		locationClause = fmt.Sprintf("postcode = $%d", zipArg)
	case cityArg > 0:
		locationClause = fmt.Sprintf("city ILIKE $%d", cityArg)
	}

	if hasStreet && locationClause != "" {
		// Tier 1: house + street in location (exact address)
		if houseArg > 0 {
			addTier(fmt.Sprintf("house_number = $%d AND %s AND %s",
				houseArg, streetClause, locationClause), true)
		}

		// Tier 2: street in location (right street, any house number)
		addTier(fmt.Sprintf("%s AND %s", streetClause, locationClause), false)
	} else if hasStreet {
		// No city/zip provided — match on street alone
		if houseArg > 0 {
			addTier(fmt.Sprintf("house_number = $%d AND %s",
				houseArg, streetClause), true)
		}
		addTier(streetClause, false)
	}

	// Tier 3: zip only (right area, any street)
	if zipArg > 0 {
		addTier(fmt.Sprintf("postcode = $%d", zipArg), false)
	}

	// Tier 4: city only (broadest location match)
	if cityArg > 0 {
		addTier(fmt.Sprintf("city ILIKE $%d", cityArg), false)
	}

	if len(tierCTEs) == 0 {
		return nil, nil
	}

	// Add limit arg
	limitArg := argNum
	args = append(args, limit)

	query := fmt.Sprintf(`
		WITH %s
		SELECT id, hash, house_number, street, unit, city, district, region, postcode, county, full_address,
			latitude, longitude, created_at, tier
		FROM (%s) combined
		ORDER BY tier, full_address
		LIMIT $%d
	`, strings.Join(tierCTEs, ",\n"), strings.Join(tierSelects, " UNION ALL "), limitArg)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute component search: %w", err)
	}
	defer rows.Close()

	result := &componentSearchResult{}
	for rows.Next() {
		var addr models.OhioAddress
		var unit, district sql.NullString
		var tier int

		err := rows.Scan(
			&addr.ID, &addr.Hash, &addr.HouseNumber, &addr.Street, &unit,
			&addr.City, &district, &addr.Region, &addr.Postcode, &addr.County, &addr.FullAddress,
			&addr.Latitude, &addr.Longitude, &addr.CreatedAt, &tier,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan component search result: %w", err)
		}

		if unit.Valid {
			addr.Unit = unit.String
		}
		if district.Valid {
			addr.District = district.String
		}

		result.Addresses = append(result.Addresses, addr)

		if exactTiers[tier] {
			result.ExactCount++
		} else {
			result.NearbyCount++
		}
		if result.BestTier == 0 || tier < result.BestTier {
			result.BestTier = tier
		}
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating component search rows: %w", err)
	}

	return result, nil
}

// searchAddressesByPrefix matches every word in the query as a prefix against
// the full_address tsvector index (migration 18). Unlike the substring path it
// does not require the words to be adjacent, so "barendt toledo" matches
// "7057 Barendt Road, Toledo, OH 43617".
func (s *AddressService) searchAddressesByPrefix(query string, limit int) ([]models.OhioAddress, error) {
	var words []string
	for _, w := range strings.Fields(query) {
		if len(sanitizeTSTerm(w)) >= 2 {
			words = append(words, w)
		}
	}
	if len(words) == 0 {
		return nil, nil
	}

	searchQuery := `
		SELECT 
			id, hash, house_number, street, unit, city, district, region, postcode, county, full_address,
			ST_Y(geom) as latitude, ST_X(geom) as longitude, created_at
		FROM ohio_addresses
		WHERE fts @@ to_tsquery('simple', $1)
		ORDER BY 
			CASE 
				WHEN full_address ILIKE $2 THEN 1  -- Contiguous match to original query
				ELSE 2
			END,
			full_address
		LIMIT $3
	`

	rows, err := s.db.Query(searchQuery, buildPrefixTSQuery(words), "%"+query+"%", limit)
	if err != nil {
		return nil, fmt.Errorf("failed to execute prefix search: %w", err)
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
// sanitizeTSTerm strips everything that is not alphanumeric so a user-supplied
// word can never be interpreted as tsquery syntax (&, |, !, parentheses, :*).
func sanitizeTSTerm(word string) string {
	var b strings.Builder
	for _, r := range word {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		}
	}
	return b.String()
}

// buildPrefixTSQuery turns the typed words into an AND-ed prefix query, so
// "1410 bare" becomes "1410:* & bare:*" and matches "1410 Barendt Road".
func buildPrefixTSQuery(words []string) string {
	terms := make([]string, 0, len(words))
	for _, w := range words {
		if s := sanitizeTSTerm(w); s != "" {
			terms = append(terms, s+":*")
		}
	}
	return strings.Join(terms, " & ")
}
