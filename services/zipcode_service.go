package services

import (
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

// LoadZipCodesFromCSV loads ZIP code data from CSV file into the database
func LoadZipCodesFromCSV(filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open CSV file: %w", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.Comma = ';' // CSV uses semicolon as delimiter
	reader.FieldsPerRecord = 17 // Expected number of fields

	// Skip header row
	_, err = reader.Read()
	if err != nil {
		return fmt.Errorf("failed to read CSV header: %w", err)
	}

	// Prepare insert statement
	stmt, err := database.DB.Prepare(`
		INSERT INTO zip_codes (
			zip_code, city_name, state_code, state_name, zcta, zcta_parent,
			population, density, primary_county_code, primary_county_name,
			county_weights, county_names, county_codes, imprecise, military,
			timezone, latitude, longitude
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
		ON CONFLICT (zip_code) DO UPDATE SET
			city_name = EXCLUDED.city_name,
			state_code = EXCLUDED.state_code,
			state_name = EXCLUDED.state_name,
			zcta = EXCLUDED.zcta,
			zcta_parent = EXCLUDED.zcta_parent,
			population = EXCLUDED.population,
			density = EXCLUDED.density,
			primary_county_code = EXCLUDED.primary_county_code,
			primary_county_name = EXCLUDED.primary_county_name,
			county_weights = EXCLUDED.county_weights,
			county_names = EXCLUDED.county_names,
			county_codes = EXCLUDED.county_codes,
			imprecise = EXCLUDED.imprecise,
			military = EXCLUDED.military,
			timezone = EXCLUDED.timezone,
			latitude = EXCLUDED.latitude,
			longitude = EXCLUDED.longitude,
			updated_at = CURRENT_TIMESTAMP
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer stmt.Close()

	recordCount := 0
	errorCount := 0

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("Error reading CSV record: %v", err)
			errorCount++
			continue
		}

		zipCode, err := parseCSVRecord(record)
		if err != nil {
			log.Printf("Error parsing record %d: %v", recordCount+1, err)
			errorCount++
			continue
		}

		err = insertZipCode(stmt, zipCode)
		if err != nil {
			log.Printf("Error inserting ZIP code %s: %v", zipCode.ZipCode, err)
			errorCount++
			continue
		}

		recordCount++
		if recordCount%1000 == 0 {
			log.Printf("Processed %d records...", recordCount)
		}
	}

	log.Printf("CSV import completed. Successfully processed: %d, Errors: %d", recordCount, errorCount)
	return nil
}

// parseCSVRecord parses a single CSV record into a ZipCode struct
func parseCSVRecord(record []string) (*models.ZipCode, error) {
	if len(record) != 17 {
		return nil, fmt.Errorf("expected 17 fields, got %d", len(record))
	}

	zipCode := &models.ZipCode{
		ZipCode:           record[0],
		CityName:          record[1],
		StateCode:         record[2],
		StateName:         record[3],
		PrimaryCountyCode: record[8],
		PrimaryCountyName: record[9],
		Timezone:          record[15],
	}

	// Parse ZCTA (boolean)
	zipCode.ZCTA = strings.ToUpper(record[4]) == "TRUE"

	// Parse ZCTA Parent (nullable string)
	if record[5] != "" {
		zipCode.ZCTAParent = &record[5]
	}

	// Parse Population (nullable float)
	if record[6] != "" {
		pop, err := strconv.ParseFloat(record[6], 64)
		if err != nil {
			return nil, fmt.Errorf("invalid population value: %s", record[6])
		}
		zipCode.Population = &pop
	}

	// Parse Density (nullable float)
	if record[7] != "" {
		density, err := strconv.ParseFloat(record[7], 64)
		if err != nil {
			return nil, fmt.Errorf("invalid density value: %s", record[7])
		}
		zipCode.Density = &density
	}

	// Parse County Weights (JSON)
	if record[10] != "" {
		weights, err := models.ParseCountyWeights(record[10])
		if err != nil {
			return nil, fmt.Errorf("invalid county weights: %s", record[10])
		}
		zipCode.CountyWeights = weights
	}

	// Parse County Names and Codes
	zipCode.CountyNames = models.ParseStringArray(record[11])
	zipCode.CountyCodes = models.ParseStringArray(record[12])

	// Parse boolean flags
	zipCode.Imprecise = strings.ToUpper(record[13]) == "TRUE"
	zipCode.Military = strings.ToUpper(record[14]) == "TRUE"

	// Parse Geo Point
	lat, lng, err := models.ParseGeoPoint(record[16])
	if err != nil {
		return nil, fmt.Errorf("invalid geo point: %s", record[16])
	}
	zipCode.Latitude = lat
	zipCode.Longitude = lng

	return zipCode, nil
}

// insertZipCode inserts a ZipCode into the database
func insertZipCode(stmt *sql.Stmt, zipCode *models.ZipCode) error {
	_, err := stmt.Exec(
		zipCode.ZipCode,
		zipCode.CityName,
		zipCode.StateCode,
		zipCode.StateName,
		zipCode.ZCTA,
		zipCode.ZCTAParent,
		zipCode.Population,
		zipCode.Density,
		zipCode.PrimaryCountyCode,
		zipCode.PrimaryCountyName,
		zipCode.CountyWeights,
		zipCode.CountyNames,
		zipCode.CountyCodes,
		zipCode.Imprecise,
		zipCode.Military,
		zipCode.Timezone,
		zipCode.Latitude,
		zipCode.Longitude,
	)
	return err
}

// GetZipCodeByZip retrieves a ZIP code by its ZIP code
func GetZipCodeByZip(zipCode string) (*models.ZipCode, error) {
	query := `
		SELECT zip_code, city_name, state_code, state_name, zcta, zcta_parent,
			   population, density, primary_county_code, primary_county_name,
			   county_weights, county_names, county_codes, imprecise, military,
			   timezone, latitude, longitude
		FROM zip_codes
		WHERE zip_code = $1
	`

	row := database.DB.QueryRow(query, zipCode)
	
	zc := &models.ZipCode{}
	err := row.Scan(
		&zc.ZipCode,
		&zc.CityName,
		&zc.StateCode,
		&zc.StateName,
		&zc.ZCTA,
		&zc.ZCTAParent,
		&zc.Population,
		&zc.Density,
		&zc.PrimaryCountyCode,
		&zc.PrimaryCountyName,
		&zc.CountyWeights,
		&zc.CountyNames,
		&zc.CountyCodes,
		&zc.Imprecise,
		&zc.Military,
		&zc.Timezone,
		&zc.Latitude,
		&zc.Longitude,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to scan ZIP code: %w", err)
	}

	return zc, nil
}

// SearchZipCodesByCity searches for ZIP codes by city name
func SearchZipCodesByCity(cityName string, stateCode string, limit int) ([]*models.ZipCode, error) {
	query := `
		SELECT zip_code, city_name, state_code, state_name, zcta, zcta_parent,
			   population, density, primary_county_code, primary_county_name,
			   county_weights, county_names, county_codes, imprecise, military,
			   timezone, latitude, longitude
		FROM zip_codes
		WHERE LOWER(city_name) LIKE LOWER($1)
	`
	
	args := []interface{}{"%" + cityName + "%"}
	
	if stateCode != "" {
		query += " AND state_code = $2"
		args = append(args, stateCode)
	}
	
	query += " ORDER BY city_name, zip_code LIMIT $" + strconv.Itoa(len(args)+1)
	args = append(args, limit)

	rows, err := database.DB.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query ZIP codes: %w", err)
	}
	defer rows.Close()

	var zipCodes []*models.ZipCode
	for rows.Next() {
		zc := &models.ZipCode{}
		err := rows.Scan(
			&zc.ZipCode,
			&zc.CityName,
			&zc.StateCode,
			&zc.StateName,
			&zc.ZCTA,
			&zc.ZCTAParent,
			&zc.Population,
			&zc.Density,
			&zc.PrimaryCountyCode,
			&zc.PrimaryCountyName,
			&zc.CountyWeights,
			&zc.CountyNames,
			&zc.CountyCodes,
			&zc.Imprecise,
			&zc.Military,
			&zc.Timezone,
			&zc.Latitude,
			&zc.Longitude,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan ZIP code: %w", err)
		}
		zipCodes = append(zipCodes, zc)
	}

	return zipCodes, nil
}

// InitializeData checks if ZIP code data exists and loads it if empty
func InitializeData() error {
	// Check if we have any data
	var count int
	err := database.DB.QueryRow("SELECT COUNT(*) FROM zip_codes").Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to count existing records: %w", err)
	}

	if count > 0 {
		log.Printf("Database already contains %d ZIP code records", count)
		return nil
	}

	log.Println("No ZIP code data found, attempting to load from CSV...")
	
	// Try to find the CSV file in common locations
	csvPaths := []string{
		"georef-united-states-of-america-zc-point.csv",
		"/app/georef-united-states-of-america-zc-point.csv",
		"./georef-united-states-of-america-zc-point.csv",
	}

	var csvPath string
	for _, path := range csvPaths {
		if _, err := os.Stat(path); err == nil {
			csvPath = path
			break
		}
	}

	if csvPath == "" {
		log.Println("CSV file not found in common locations. You can load data manually using the /api/v1/admin/load-data endpoint")
		return nil
	}

	log.Printf("Found CSV file at: %s", csvPath)
	return LoadZipCodesFromCSV(csvPath)
}