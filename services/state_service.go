package services

import (
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"geocoding-api/database"
	"geocoding-api/models"
)

// StateService handles state-related operations
type StateService struct{}

var State = &StateService{}

// InitializeStateData loads state data from GeoJSON if the table is empty
func InitializeStateData() error {
	var count int
	err := database.DB.QueryRow("SELECT COUNT(*) FROM us_states").Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check us_states table: %w", err)
	}

	if count > 0 {
		log.Printf("States table already contains %d records, skipping initialization", count)
		return nil
	}

	log.Println("States table is empty, loading data from tl_2025_us_state.geojson.gz...")
	
	file, err := os.Open("tl_2025_us_state.geojson.gz")
	if err != nil {
		return fmt.Errorf("failed to open tl_2025_us_state.geojson.gz: %w", err)
	}
	defer file.Close()

	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzReader.Close()

	// Read the entire GeoJSON
	var geoJSON struct {
		Type     string `json:"type"`
		Features []struct {
			Type     string `json:"type"`
			Geometry struct {
				Type        string          `json:"type"`
				Coordinates json.RawMessage `json:"coordinates"`
			} `json:"geometry"`
			Properties struct {
				STATEFP   string `json:"STATEFP"`
				STATENS   string `json:"STATENS"`
				GEOID     string `json:"GEOID"`
				STUSPS    string `json:"STUSPS"`
				NAME      string `json:"NAME"`
				LSAD      string `json:"LSAD"`
				MTFCC     string `json:"MTFCC"`
				FUNCSTAT  string `json:"FUNCSTAT"`
				ALAND     int64  `json:"ALAND"`
				AWATER    int64  `json:"AWATER"`
				INTPTLAT  string `json:"INTPTLAT"`
				INTPTLON  string `json:"INTPTLON"`
				REGION    string `json:"REGION"`
				DIVISION  string `json:"DIVISION"`
			} `json:"properties"`
		} `json:"features"`
	}

	decoder := json.NewDecoder(gzReader)
	if err := decoder.Decode(&geoJSON); err != nil {
		return fmt.Errorf("failed to decode GeoJSON: %w", err)
	}

	log.Printf("Loaded %d state features from GeoJSON", len(geoJSON.Features))

	// Prepare insert statement
	stmt, err := database.DB.Prepare(`
		INSERT INTO us_states (
			state_fips, state_abbr, state_name, state_ns, geoid,
			region, division, lsad, mtfcc, funcstat,
			area_land, area_water, internal_lat, internal_lng, geometry
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
			ST_GeomFromGeoJSON($15)
		)
		ON CONFLICT (state_fips) DO NOTHING
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer stmt.Close()

	count = 0
	skipped := 0
	
	for _, feature := range geoJSON.Features {
		props := feature.Properties
		
		// Parse internal point coordinates
		var internalLat, internalLng float64
		fmt.Sscanf(props.INTPTLAT, "%f", &internalLat)
		fmt.Sscanf(props.INTPTLON, "%f", &internalLng)

		// Create a GeoJSON geometry string for PostGIS
		geometryJSON := fmt.Sprintf(`{"type":"%s","coordinates":%s}`, 
			feature.Geometry.Type, 
			string(feature.Geometry.Coordinates))

		_, err := stmt.Exec(
			props.STATEFP,
			props.STUSPS,
			props.NAME,
			props.STATENS,
			props.GEOID,
			props.REGION,
			props.DIVISION,
			props.LSAD,
			props.MTFCC,
			props.FUNCSTAT,
			props.ALAND,
			props.AWATER,
			internalLat,
			internalLng,
			geometryJSON,
		)

		if err != nil {
			log.Printf("Failed to insert state %s: %v", props.NAME, err)
			skipped++
			continue
		}
		
		count++
	}

	log.Printf("Successfully loaded %d states (%d skipped)", count, skipped)
	return nil
}

// SearchStates searches for states by name or abbreviation
func (ss *StateService) SearchStates(params models.StateSearchParams) (*models.StateSearchResponse, error) {
	query := `
		SELECT id, state_fips, state_abbr, state_name, state_ns, geoid,
			   region, division, lsad, mtfcc, funcstat,
			   area_land, area_water, internal_lat, internal_lng, created_at
		FROM us_states
		WHERE 1=1
	`
	
	var conditions []string
	var args []interface{}
	argIndex := 1

	// Add search conditions
	if params.Name != "" {
		conditions = append(conditions, fmt.Sprintf("LOWER(state_name) LIKE LOWER($%d)", argIndex))
		args = append(args, "%"+params.Name+"%")
		argIndex++
	}

	if params.Abbr != "" {
		conditions = append(conditions, fmt.Sprintf("UPPER(state_abbr) = UPPER($%d)", argIndex))
		args = append(args, params.Abbr)
		argIndex++
	}

	if params.Region != "" {
		conditions = append(conditions, fmt.Sprintf("region = $%d", argIndex))
		args = append(args, params.Region)
		argIndex++
	}

	if params.Division != "" {
		conditions = append(conditions, fmt.Sprintf("division = $%d", argIndex))
		args = append(args, params.Division)
		argIndex++
	}

	// Add conditions to query
	if len(conditions) > 0 {
		query += " AND " + strings.Join(conditions, " AND ")
	}

	// Add ordering
	query += " ORDER BY state_name ASC"

	// Add pagination
	if params.Limit <= 0 {
		params.Limit = 50
	}
	query += fmt.Sprintf(" LIMIT $%d", argIndex)
	args = append(args, params.Limit)
	argIndex++

	if params.Offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", argIndex)
		args = append(args, params.Offset)
	}

	rows, err := database.DB.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query states: %w", err)
	}
	defer rows.Close()

	var states []models.State
	for rows.Next() {
		var state models.State
		var stateNS, geoid, region, division, lsad, mtfcc, funcstat sql.NullString
		var areaLand, areaWater sql.NullInt64
		var internalLat, internalLng sql.NullFloat64

		err := rows.Scan(
			&state.ID, &state.StateFIPS, &state.StateAbbr, &state.StateName,
			&stateNS, &geoid, &region, &division, &lsad, &mtfcc, &funcstat,
			&areaLand, &areaWater, &internalLat, &internalLng, &state.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan state: %w", err)
		}

		if stateNS.Valid {
			state.StateNS = stateNS.String
		}
		if geoid.Valid {
			state.GeoID = geoid.String
		}
		if region.Valid {
			state.Region = region.String
		}
		if division.Valid {
			state.Division = division.String
		}
		if lsad.Valid {
			state.LSAD = lsad.String
		}
		if mtfcc.Valid {
			state.MTFCC = mtfcc.String
		}
		if funcstat.Valid {
			state.FuncStat = funcstat.String
		}
		if areaLand.Valid {
			state.AreaLand = areaLand.Int64
		}
		if areaWater.Valid {
			state.AreaWater = areaWater.Int64
		}
		if internalLat.Valid {
			state.InternalLat = internalLat.Float64
		}
		if internalLng.Valid {
			state.InternalLng = internalLng.Float64
		}

		states = append(states, state)
	}

	// Get total count
	var total int
	countQuery := `SELECT COUNT(*) FROM us_states WHERE 1=1`
	if len(conditions) > 0 {
		countQuery += " AND " + strings.Join(conditions, " AND ")
	}
	err = database.DB.QueryRow(countQuery, args[:len(args)-2]...).Scan(&total)
	if err != nil {
		total = len(states)
	}

	return &models.StateSearchResponse{
		States: states,
		Total:  total,
		Limit:  params.Limit,
		Offset: params.Offset,
	}, nil
}

// GetStateByIdentifier gets a state by FIPS code, abbreviation, or name
func (ss *StateService) GetStateByIdentifier(identifier string) (*models.State, error) {
	query := `
		SELECT id, state_fips, state_abbr, state_name, state_ns, geoid,
			   region, division, lsad, mtfcc, funcstat,
			   area_land, area_water, internal_lat, internal_lng, created_at
		FROM us_states
		WHERE state_fips = $1 OR UPPER(state_abbr) = UPPER($1) OR LOWER(state_name) = LOWER($1)
		LIMIT 1
	`

	var state models.State
	var stateNS, geoid, region, division, lsad, mtfcc, funcstat sql.NullString
	var areaLand, areaWater sql.NullInt64
	var internalLat, internalLng sql.NullFloat64

	err := database.DB.QueryRow(query, identifier).Scan(
		&state.ID, &state.StateFIPS, &state.StateAbbr, &state.StateName,
		&stateNS, &geoid, &region, &division, &lsad, &mtfcc, &funcstat,
		&areaLand, &areaWater, &internalLat, &internalLng, &state.CreatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("state not found: %s", identifier)
		}
		return nil, fmt.Errorf("failed to query state: %w", err)
	}

	if stateNS.Valid {
		state.StateNS = stateNS.String
	}
	if geoid.Valid {
		state.GeoID = geoid.String
	}
	if region.Valid {
		state.Region = region.String
	}
	if division.Valid {
		state.Division = division.String
	}
	if lsad.Valid {
		state.LSAD = lsad.String
	}
	if mtfcc.Valid {
		state.MTFCC = mtfcc.String
	}
	if funcstat.Valid {
		state.FuncStat = funcstat.String
	}
	if areaLand.Valid {
		state.AreaLand = areaLand.Int64
	}
	if areaWater.Valid {
		state.AreaWater = areaWater.Int64
	}
	if internalLat.Valid {
		state.InternalLat = internalLat.Float64
	}
	if internalLng.Valid {
		state.InternalLng = internalLng.Float64
	}

	return &state, nil
}

// GetStateBoundaryGeoJSON returns the state boundary as GeoJSON
func (ss *StateService) GetStateBoundaryGeoJSON(identifier string) (map[string]interface{}, error) {
	query := `
		SELECT state_abbr, state_name, state_fips, area_land, area_water,
			   ST_AsGeoJSON(geometry)::json as geometry
		FROM us_states
		WHERE state_fips = $1 OR UPPER(state_abbr) = UPPER($1) OR LOWER(state_name) = LOWER($1)
		LIMIT 1
	`

	var stateAbbr, stateName, stateFIPS string
	var areaLand, areaWater int64
	var geometryJSON json.RawMessage

	err := database.DB.QueryRow(query, identifier).Scan(
		&stateAbbr, &stateName, &stateFIPS, &areaLand, &areaWater, &geometryJSON,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("state not found: %s", identifier)
		}
		return nil, fmt.Errorf("failed to query state boundary: %w", err)
	}

	// Parse the geometry JSON
	var geometry map[string]interface{}
	if err := json.Unmarshal(geometryJSON, &geometry); err != nil {
		return nil, fmt.Errorf("failed to parse geometry: %w", err)
	}

	// Build GeoJSON feature
	feature := map[string]interface{}{
		"type": "Feature",
		"properties": map[string]interface{}{
			"state_abbr": stateAbbr,
			"state_name": stateName,
			"state_fips": stateFIPS,
			"area_land":  areaLand,
			"area_water": areaWater,
		},
		"geometry": geometry,
	}

	return feature, nil
}

// GetStateByCoordinates finds which state contains the given coordinates
func (ss *StateService) GetStateByCoordinates(lat, lng float64) (*models.State, error) {
	query := `
		SELECT id, state_fips, state_abbr, state_name, state_ns, geoid,
			   region, division, lsad, mtfcc, funcstat,
			   area_land, area_water, internal_lat, internal_lng, created_at
		FROM us_states
		WHERE ST_Contains(geometry, ST_SetSRID(ST_MakePoint($1, $2), 4326))
		LIMIT 1
	`

	var state models.State
	var stateNS, geoid, region, division, lsad, mtfcc, funcstat sql.NullString
	var areaLand, areaWater sql.NullInt64
	var internalLat, internalLng sql.NullFloat64

	err := database.DB.QueryRow(query, lng, lat).Scan(
		&state.ID, &state.StateFIPS, &state.StateAbbr, &state.StateName,
		&stateNS, &geoid, &region, &division, &lsad, &mtfcc, &funcstat,
		&areaLand, &areaWater, &internalLat, &internalLng, &state.CreatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("no state found at coordinates: %f, %f", lat, lng)
		}
		return nil, fmt.Errorf("failed to query state by coordinates: %w", err)
	}

	if stateNS.Valid {
		state.StateNS = stateNS.String
	}
	if geoid.Valid {
		state.GeoID = geoid.String
	}
	if region.Valid {
		state.Region = region.String
	}
	if division.Valid {
		state.Division = division.String
	}
	if lsad.Valid {
		state.LSAD = lsad.String
	}
	if mtfcc.Valid {
		state.MTFCC = mtfcc.String
	}
	if funcstat.Valid {
		state.FuncStat = funcstat.String
	}
	if areaLand.Valid {
		state.AreaLand = areaLand.Int64
	}
	if areaWater.Valid {
		state.AreaWater = areaWater.Int64
	}
	if internalLat.Valid {
		state.InternalLat = internalLat.Float64
	}
	if internalLng.Valid {
		state.InternalLng = internalLng.Float64
	}

	return &state, nil
}
