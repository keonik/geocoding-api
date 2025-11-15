package services

import (
	"compress/gzip"
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"

	"geocoding-api/database"
	"geocoding-api/models"
)

// CityService handles city-related operations
type CityService struct{}

var City = &CityService{}

// InitializeCityData loads city data from CSV if the table is empty
func InitializeCityData() error {
	var count int
	err := database.DB.QueryRow("SELECT COUNT(*) FROM cities").Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check cities table: %w", err)
	}

	if count > 0 {
		log.Printf("Cities table already contains %d records, skipping initialization", count)
		return nil
	}

	log.Println("Cities table is empty, loading data from uscities.csv.gz...")
	
	file, err := os.Open("uscities.csv.gz")
	if err != nil {
		return fmt.Errorf("failed to open uscities.csv.gz: %w", err)
	}
	defer file.Close()

	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzReader.Close()

	csvReader := csv.NewReader(gzReader)
	
	// Read header
	header, err := csvReader.Read()
	if err != nil {
		return fmt.Errorf("failed to read CSV header: %w", err)
	}
	log.Printf("CSV columns: %v", header)

	// Prepare insert statement
	stmt, err := database.DB.Prepare(`
		INSERT INTO cities (
			city, city_ascii, state_id, state_name, county_fips, county_name,
			lat, lng, population, density, source, military, incorporated,
			timezone, ranking, zips, external_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		ON CONFLICT (city_ascii, state_id) DO NOTHING
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer stmt.Close()

	count = 0
	skipped := 0
	
	for {
		record, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("Error reading CSV row: %v", err)
			skipped++
			continue
		}

		if len(record) < 17 {
			log.Printf("Skipping row with insufficient columns: %v", record)
			skipped++
			continue
		}

		// Parse numeric fields
		lat, _ := strconv.ParseFloat(record[6], 64)
		lng, _ := strconv.ParseFloat(record[7], 64)
		population, _ := strconv.Atoi(record[8])
		density, _ := strconv.ParseFloat(record[9], 64)
		ranking, _ := strconv.Atoi(record[14])
		military := strings.ToUpper(record[11]) == "TRUE"
		incorporated := strings.ToUpper(record[12]) == "TRUE"

		_, err = stmt.Exec(
			record[0],  // city
			record[1],  // city_ascii
			record[2],  // state_id
			record[3],  // state_name
			record[4],  // county_fips
			record[5],  // county_name
			lat,        // lat
			lng,        // lng
			population, // population
			density,    // density
			record[10], // source
			military,   // military
			incorporated, // incorporated
			record[13], // timezone
			ranking,    // ranking
			record[15], // zips
			record[16], // external_id
		)
		if err != nil {
			log.Printf("Error inserting city %s, %s: %v", record[0], record[2], err)
			skipped++
			continue
		}

		count++
		if count%1000 == 0 {
			log.Printf("Loaded %d cities...", count)
		}
	}

	log.Printf("✅ Successfully loaded %d cities (skipped %d)", count, skipped)
	return nil
}

// SearchCities searches for cities based on various parameters
func (cs *CityService) SearchCities(params models.CitySearchParams) ([]models.City, int, error) {
	if params.Limit <= 0 {
		params.Limit = 10
	}
	if params.Limit > 100 {
		params.Limit = 100
	}

	var conditions []string
	var args []interface{}
	argCount := 0

	// Build WHERE clause
	if params.Query != "" {
		argCount++
		conditions = append(conditions, fmt.Sprintf("(city_ascii ILIKE $%d OR city ILIKE $%d)", argCount, argCount))
		args = append(args, "%"+params.Query+"%")
	}

	if params.City != "" {
		argCount++
		conditions = append(conditions, fmt.Sprintf("city_ascii ILIKE $%d", argCount))
		args = append(args, "%"+params.City+"%")
	}

	if params.State != "" {
		argCount++
		// Check if state is 2-letter code or full name
		stateUpper := strings.ToUpper(params.State)
		if len(params.State) == 2 {
			// Likely state_id (e.g., "OH")
			conditions = append(conditions, fmt.Sprintf("state_id = $%d", argCount))
			args = append(args, stateUpper)
		} else {
			// Likely state_name (e.g., "Ohio") - check both to be flexible
			conditions = append(conditions, fmt.Sprintf("(state_id = $%d OR state_name ILIKE $%d)", argCount, argCount))
			args = append(args, stateUpper)
		}
	}

	if params.County != "" {
		argCount++
		conditions = append(conditions, fmt.Sprintf("county_name ILIKE $%d", argCount))
		args = append(args, "%"+params.County+"%")
	}

	if params.MinPop > 0 {
		argCount++
		conditions = append(conditions, fmt.Sprintf("population >= $%d", argCount))
		args = append(args, params.MinPop)
	}

	// Location-based search
	if params.Lat != 0 && params.Lng != 0 {
		if params.Radius > 0 {
			// Using simple distance calculation (good enough for most cases)
			argCount += 3
			conditions = append(conditions, fmt.Sprintf(`
				(6371 * acos(
					cos(radians($%d)) * cos(radians(lat)) * 
					cos(radians(lng) - radians($%d)) + 
					sin(radians($%d)) * sin(radians(lat))
				)) <= $%d
			`, argCount-2, argCount-1, argCount-2, argCount))
			args = append(args, params.Lat, params.Lng, params.Radius)
		}
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	// Get total count
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM cities %s", whereClause)
	var total int
	err := database.DB.QueryRow(countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count cities: %w", err)
	}

	// Build main query
	query := fmt.Sprintf(`
		SELECT id, city, city_ascii, state_id, state_name, county_fips, county_name,
		       lat, lng, population, density, source, military, incorporated,
		       timezone, ranking, zips, external_id
		FROM cities
		%s
		ORDER BY 
			CASE WHEN ranking > 0 THEN ranking ELSE 999999 END,
			population DESC NULLS LAST
		LIMIT $%d OFFSET $%d
	`, whereClause, argCount+1, argCount+2)

	args = append(args, params.Limit, params.Offset)

	rows, err := database.DB.Query(query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query cities: %w", err)
	}
	defer rows.Close()

	var cities []models.City
	for rows.Next() {
		var city models.City
		var countyFIPS, countyName, source, timezone, zips, externalID sql.NullString
		var population, ranking sql.NullInt64
		var density sql.NullFloat64

		err := rows.Scan(
			&city.ID, &city.City, &city.CityAscii, &city.StateID, &city.StateName,
			&countyFIPS, &countyName, &city.Lat, &city.Lng,
			&population, &density, &source, &city.Military, &city.Incorporated,
			&timezone, &ranking, &zips, &externalID,
		)
		if err != nil {
			log.Printf("Error scanning city: %v", err)
			continue
		}

		if countyFIPS.Valid {
			city.CountyFIPS = countyFIPS.String
		}
		if countyName.Valid {
			city.CountyName = countyName.String
		}
		if population.Valid {
			city.Population = int(population.Int64)
		}
		if density.Valid {
			city.Density = density.Float64
		}
		if source.Valid {
			city.Source = source.String
		}
		if timezone.Valid {
			city.Timezone = timezone.String
		}
		if ranking.Valid {
			city.Ranking = int(ranking.Int64)
		}
		if zips.Valid {
			city.Zips = zips.String
		}
		if externalID.Valid {
			city.ExternalID = externalID.String
		}

		cities = append(cities, city)
	}

	return cities, total, nil
}

// GetCityByID retrieves a specific city by ID
func (cs *CityService) GetCityByID(id int64) (*models.City, error) {
	var city models.City
	var countyFIPS, countyName, source, timezone, zips, externalID sql.NullString
	var population, ranking sql.NullInt64
	var density sql.NullFloat64

	query := `
		SELECT id, city, city_ascii, state_id, state_name, county_fips, county_name,
		       lat, lng, population, density, source, military, incorporated,
		       timezone, ranking, zips, external_id
		FROM cities
		WHERE id = $1
	`

	err := database.DB.QueryRow(query, id).Scan(
		&city.ID, &city.City, &city.CityAscii, &city.StateID, &city.StateName,
		&countyFIPS, &countyName, &city.Lat, &city.Lng,
		&population, &density, &source, &city.Military, &city.Incorporated,
		&timezone, &ranking, &zips, &externalID,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("city not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get city: %w", err)
	}

	if countyFIPS.Valid {
		city.CountyFIPS = countyFIPS.String
	}
	if countyName.Valid {
		city.CountyName = countyName.String
	}
	if population.Valid {
		city.Population = int(population.Int64)
	}
	if density.Valid {
		city.Density = density.Float64
	}
	if source.Valid {
		city.Source = source.String
	}
	if timezone.Valid {
		city.Timezone = timezone.String
	}
	if ranking.Valid {
		city.Ranking = int(ranking.Int64)
	}
	if zips.Valid {
		city.Zips = zips.String
	}
	if externalID.Valid {
		city.ExternalID = externalID.String
	}

	return &city, nil
}

// GetZIPCodesForCity returns the list of ZIP codes for a city
func (cs *CityService) GetZIPCodesForCity(cityAscii, state string) ([]string, error) {
	var zips sql.NullString
	var query string
	
	// Handle state as either state_id (2 chars) or state_name
	stateUpper := strings.ToUpper(state)
	if len(state) == 2 {
		// State ID like "OH"
		query = "SELECT zips FROM cities WHERE city_ascii = $1 AND state_id = $2"
	} else {
		// State name like "Ohio" - check both state_id and state_name
		query = "SELECT zips FROM cities WHERE city_ascii = $1 AND (state_id = $2 OR state_name ILIKE $2)"
	}
	
	err := database.DB.QueryRow(query, cityAscii, stateUpper).Scan(&zips)
	if err == sql.ErrNoRows {
		return []string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get ZIP codes: %w", err)
	}

	if !zips.Valid || zips.String == "" {
		return []string{}, nil
	}

	// Split ZIP codes by space
	zipList := strings.Fields(zips.String)
	return zipList, nil
}
