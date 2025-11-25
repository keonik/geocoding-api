package services

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"geocoding-api/database"
	"geocoding-api/utils"
)

// InitializeOhioData checks if Ohio address data exists and logs status
// NOTE: With the new data upload system, this function no longer auto-loads data from files
// Admins should use the Data Manager UI at /data-manager to upload county datasets
func InitializeOhioData() error {
	// Check total count first
	var totalCount int
	err := database.DB.QueryRow("SELECT COUNT(*) FROM ohio_addresses").Scan(&totalCount)
	if err != nil {
		return fmt.Errorf("failed to count existing Ohio address records: %w", err)
	}

	if totalCount == 0 {
		log.Println("No address data found in database.")
		log.Println("Use the Data Manager UI at /data-manager to upload county address datasets.")
		return nil
	}

	// Get list of counties already loaded
	rows, err := database.DB.Query("SELECT DISTINCT county FROM ohio_addresses")
	if err != nil {
		return fmt.Errorf("failed to query existing counties: %w", err)
	}
	defer rows.Close()
	
	var loadedCounties []string
	for rows.Next() {
		var county string
		if err := rows.Scan(&county); err != nil {
			continue
		}
		loadedCounties = append(loadedCounties, county)
	}
	
	log.Printf("Database contains %d address records across %d counties", totalCount, len(loadedCounties))
	
	return nil
}

// LoadOhioAddressData loads address data from all Ohio county GeoJSON files
func LoadOhioAddressData() error {
	return loadMissingCounties(make(map[string]bool))
}

// loadMissingCounties loads data for counties not already in the database
func loadMissingCounties(loadedCounties map[string]bool) error {
	log.Println("Loading Ohio address data from GeoJSON files...")
	
	destDir := "."
	ohDir := filepath.Join(destDir, "oh")
	
	// Create oh directory if it doesn't exist
	if err := os.MkdirAll(ohDir, 0755); err != nil {
		return fmt.Errorf("failed to create ohio data directory: %w", err)
	}

	// Get list of all Ohio counties
	counties := utils.GetOhioCountyList()
	
	totalRecords := 0
	successfulCounties := 0
	skippedCounties := 0
	
	for _, county := range counties {
		// Skip if already loaded
		if loadedCounties[strings.ToLower(county)] {
			skippedCounties++
			continue
		}
		
		addressFile := filepath.Join(ohDir, fmt.Sprintf("%s-addresses-county.geojson", county))
		
		// Decompress if needed (lazy decompression)
		if err := decompressIfNeeded(addressFile); err != nil {
			log.Printf("Failed to decompress %s: %v", county, err)
			continue
		}
		
		// Check if file exists after decompression attempt
		if _, err := os.Stat(addressFile); os.IsNotExist(err) {
			log.Printf("GeoJSON file not found for %s, skipping (no compressed file available)", county)
			continue
		}
		
		// Load county data
		count, err := loadCountyAddresses(county, addressFile)
		if err != nil {
			log.Printf("Warning: Failed to load %s: %v", county, err)
			continue
		}
		
		totalRecords += count
		successfulCounties++
		
		if count > 0 {
			log.Printf("Loaded %d records from %s", count, strings.Title(county))
		} else {
			// Check if it's a placeholder file with ArcGIS source
			content, readErr := os.ReadFile(addressFile)
			if readErr != nil {
				log.Printf("Loaded 0 records from %s (could not read file: %v)", strings.Title(county), readErr)
			} else {
				contentStr := string(content)
				
				// Check for ArcGIS indicators
				if strings.Contains(contentStr, "FeatureServer") {
					log.Printf("Info: %s uses ArcGIS FeatureServer (not yet supported)", strings.Title(county))
				} else if len(contentStr) < 500 && strings.Contains(contentStr, `"features": []`) {
					log.Printf("Info: %s has empty placeholder file", strings.Title(county))
				} else {
					log.Printf("Loaded 0 records from %s (no features in shapefile - may be empty)", strings.Title(county))
				}
			}
		}
	}
	
	if skippedCounties > 0 {
		log.Printf("Skipped %d counties (already loaded)", skippedCounties)
	}
	log.Printf("Completed loading Ohio address data: %d records from %d counties", totalRecords, successfulCounties)
	
	// Clean up GeoJSON files after successful loading to save disk space
	if err := cleanupGeoJSONFiles(); err != nil {
		log.Printf("Warning: Failed to cleanup GeoJSON files: %v", err)
		// Don't return error as the loading was successful
	}
	
	return nil
}

// cleanupGeoJSONFiles removes GeoJSON and meta files after data has been loaded into database
func cleanupGeoJSONFiles() error {
	log.Println("Cleaning up GeoJSON files to save disk space...")
	
	// Check if we're in production environment
	isProd := os.Getenv("ENV") == "production" || os.Getenv("GO_ENV") == "production"
	
	// Also check if CLEANUP_GEOJSON is explicitly set
	cleanupEnabled := os.Getenv("CLEANUP_GEOJSON") == "true"
	
	if !isProd && !cleanupEnabled {
		log.Println("Skipping GeoJSON cleanup in development environment. Set CLEANUP_GEOJSON=true to force cleanup.")
		return nil
	}
	
	// Get all GeoJSON files (both .geojson and .geojson.meta files)
	patterns := []string{
		"oh/*.geojson",
		"oh/*.geojson.meta",
	}
	
	totalFilesDeleted := 0
	var totalSizeFreed int64
	
	for _, pattern := range patterns {
		files, err := filepath.Glob(pattern)
		if err != nil {
			log.Printf("Warning: Failed to find files with pattern %s: %v", pattern, err)
			continue
		}
		
		for _, filePath := range files {
			// Get file size before deletion
			if info, err := os.Stat(filePath); err == nil {
				totalSizeFreed += info.Size()
			}
			
			// Delete the file
			if err := os.Remove(filePath); err != nil {
				log.Printf("Warning: Failed to delete %s: %v", filePath, err)
				continue
			}
			
			totalFilesDeleted++
		}
	}
	
	// Convert bytes to human readable format
	sizeFreedMB := float64(totalSizeFreed) / (1024 * 1024)
	
	log.Printf("Successfully cleaned up %d GeoJSON files, freed %.2f MB of disk space", 
		totalFilesDeleted, sizeFreedMB)
	
	// Remove the oh directory if it's empty
	if entries, err := os.ReadDir("oh"); err == nil && len(entries) == 0 {
		if err := os.Remove("oh"); err != nil {
			log.Printf("Warning: Failed to remove empty oh directory: %v", err)
		} else {
			log.Println("Removed empty oh directory")
		}
	}
	
	return nil
}

// loadCountyAddresses loads address data from a single county GeoJSON file
func loadCountyAddresses(county, filePath string) (int, error) {
	// Loading address file
	
	// Open and read the GeoJSON file
	file, err := os.Open(filePath)
	if err != nil {
		return 0, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Try to detect format - peek at first bytes
	firstBytes := make([]byte, 100)
	n, err := file.Read(firstBytes)
	if err != nil {

		return 0, fmt.Errorf("failed to read file: %w", err)
	}
	file.Seek(0, 0) // Reset to beginning
	
	firstLine := string(firstBytes[:n])
	isNDJSON := strings.HasPrefix(strings.TrimSpace(firstLine), `{"type":"Feature"`) || 
	            strings.HasPrefix(strings.TrimSpace(firstLine), `{"type": "Feature"`)
	
	previewLen := 50
	if len(firstLine) < previewLen {
		previewLen = len(firstLine)
	}
	// Detect format and parse accordingly
	
	var features []struct {
		Type     string `json:"type"`
		Geometry struct {
			Type        string    `json:"type"`
			Coordinates []float64 `json:"coordinates"`
		} `json:"geometry"`
		Properties map[string]interface{} `json:"properties"`
	}

	if isNDJSON {
		// Parse newline-delimited JSON

		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024) // 10MB max line size
		

		lineCount := 0
		for scanner.Scan() {
			lineCount++
			var feature struct {
				Type     string `json:"type"`
				Geometry struct {
					Type        string    `json:"type"`
					Coordinates []float64 `json:"coordinates"`
				} `json:"geometry"`
				Properties map[string]interface{} `json:"properties"`
			}
			
			if err := json.Unmarshal(scanner.Bytes(), &feature); err != nil {
				if lineCount <= 3 {

				}
				continue // Skip malformed lines
			}
			
			features = append(features, feature)
		}
		

		
		if err := scanner.Err(); err != nil {
			return 0, fmt.Errorf("failed to scan NDJSON file: %w", err)
		}
	} else {
		// Parse FeatureCollection format
		var geoJSON struct {
			Type     string `json:"type"`
			Features []struct {
				Type     string `json:"type"`
				Geometry struct {
					Type        string    `json:"type"`
					Coordinates []float64 `json:"coordinates"`
				} `json:"geometry"`
				Properties map[string]interface{} `json:"properties"`
			} `json:"features"`
		}

		decoder := json.NewDecoder(file)
		if err := decoder.Decode(&geoJSON); err != nil {
			return 0, fmt.Errorf("failed to parse GeoJSON: %w", err)
		}
		
		features = geoJSON.Features
	}

	if len(features) == 0 {
		return 0, nil
	}

	// Prepare batch insert - using actual table schema from migration
	stmt, err := database.DB.Prepare(`
		INSERT INTO ohio_addresses (
			hash, house_number, street, unit, city, district, region, postcode, county, geom
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, ST_SetSRID(ST_MakePoint($10, $11), 4326))
		ON CONFLICT (hash) DO NOTHING
	`)
	if err != nil {
		return 0, fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	insertedCount := 0

	for _, feature := range features {
		if feature.Geometry.Type != "Point" || len(feature.Geometry.Coordinates) < 2 {
			continue
		}

		// Extract properties
		props := feature.Properties
		
		// Get coordinates (GeoJSON is [longitude, latitude])
		longitude := feature.Geometry.Coordinates[0]
		latitude := feature.Geometry.Coordinates[1]

		// Extract address components with various possible field names from Ohio LBRS shapefiles and OpenAddresses
	houseNumber := getStringProperty(props, "number", "HOUSENUM", "HouseNum", "house_number", "housenumber")
	streetName := getStringProperty(props, "street", "ST_NAME", "StreetName", "street_name", "STREETNAME", "LSN")
	unit := getStringProperty(props, "unit", "UNITNUM", "Unit", "UNIT")
	city := getStringProperty(props, "city", "USPS_CITY", "City", "CITY", "MUNI")
	state := getStringProperty(props, "region", "STATE", "State", "state", "REGION")
	// Truncate state to 2 characters to match database schema VARCHAR(2)
	if len(state) > 2 {
		state = state[:2]
	}
	zipCode := getStringProperty(props, "postcode", "ZIPCODE", "ZipCode", "zip_code", "POSTCODE")		
		// Use existing hash if available (OpenAddresses format), otherwise generate one
		hash := getStringProperty(props, "hash")
		if hash == "" {
			hash = fmt.Sprintf("%s_%s_%s_%f_%f", county, houseNumber, streetName, latitude, longitude)
		}
		
		// Skip if no meaningful address data
		if houseNumber == "" && streetName == "" {
			continue
		}

		// Insert record - matching migration schema
		_, err = stmt.Exec(
			hash,
			houseNumber,
			streetName,
			unit,
			city,
			"", // district - not in Ohio LBRS data
			state,
			zipCode,
			strings.Title(county),
			longitude, // PostGIS point needs lon, lat
			latitude,
		)
		if err != nil {
			// Skip duplicate key errors silently
			if !strings.Contains(err.Error(), "duplicate key") {
				log.Printf("Warning: Failed to insert record for %s %s: %v", houseNumber, streetName, err)
			}
			continue
		}

		insertedCount++
	}

	return insertedCount, nil
}

// getStringProperty extracts a string property from a map, trying multiple possible keys
func getStringProperty(props map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if val, ok := props[key]; ok && val != nil {
			switch v := val.(type) {
			case string:
				return strings.TrimSpace(v)
			case float64:
				return fmt.Sprintf("%.0f", v)
			case int:
				return fmt.Sprintf("%d", v)
			}
		}
	}
	return ""
}

// decompressIfNeeded decompresses a .geojson.gz file if the .geojson doesn't exist
func decompressIfNeeded(geojsonPath string) error {
	// If decompressed file already exists, nothing to do
	if _, err := os.Stat(geojsonPath); err == nil {
		return nil
	}
	
	// Check if compressed version exists
	compressedPath := geojsonPath + ".gz"
	if _, err := os.Stat(compressedPath); os.IsNotExist(err) {
		// Neither compressed nor decompressed file exists
		return nil
	}
	
	log.Printf("Decompressing %s...", filepath.Base(compressedPath))
	
	// Open compressed file
	compressedFile, err := os.Open(compressedPath)
	if err != nil {
		return fmt.Errorf("failed to open compressed file: %w", err)
	}
	defer compressedFile.Close()
	
	// Create gzip reader
	gzReader, err := gzip.NewReader(compressedFile)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzReader.Close()
	
	// Create output file
	outputFile, err := os.Create(geojsonPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer outputFile.Close()
	
	// Copy decompressed data
	if _, err := io.Copy(outputFile, gzReader); err != nil {
		// Clean up partial file on error
		os.Remove(geojsonPath)
		return fmt.Errorf("failed to decompress: %w", err)
	}
	
	log.Printf("Successfully decompressed %s", filepath.Base(geojsonPath))
	return nil
}
