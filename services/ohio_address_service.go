package services

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"geocoding-api/database"
	"geocoding-api/utils"
)

// InitializeOhioData checks if Ohio address data exists and loads it if empty
func InitializeOhioData() error {
	// Check if we have any data
	var count int
	err := database.DB.QueryRow("SELECT COUNT(*) FROM ohio_addresses").Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to count existing Ohio address records: %w", err)
	}

	if count > 0 {
		log.Printf("Database already contains %d Ohio address records", count)
		return nil
	}

	log.Println("No Ohio address data found, attempting to load from GeoJSON files...")
	return LoadOhioAddressData()
}

// LoadOhioAddressData loads address data from all Ohio county GeoJSON files
func LoadOhioAddressData() error {
	log.Println("Loading Ohio address data from GeoJSON files...")
	
	// Download/generate data if needed
	destDir := "."
	ohDir := filepath.Join(destDir, "oh")
	
	// Always trigger download if directory doesn't exist or is empty
	needsDownload := false
	if _, err := os.Stat(ohDir); os.IsNotExist(err) {
		needsDownload = true
		log.Println("Ohio data directory not found, will download...")
	} else {
		// Check if directory is empty
		entries, err := os.ReadDir(ohDir)
		if err != nil || len(entries) == 0 {
			needsDownload = true
			log.Println("Ohio data directory is empty, will download...")
		}
	}
	
	if needsDownload {
		log.Println("Downloading Ohio county data...")
		downloader := utils.NewRealDataDownloader("cache")
		if err := downloader.DownloadOhioRealData(destDir); err != nil {
			return fmt.Errorf("failed to download Ohio data: %w", err)
		}
	}

	// Get list of all Ohio counties
	counties := utils.GetOhioCountyList()
	
	totalRecords := 0
	successfulCounties := 0
	
	for _, county := range counties {
		addressFile := filepath.Join(ohDir, fmt.Sprintf("%s-addresses-county.geojson", county))
		
		// Check if file exists
		if _, err := os.Stat(addressFile); os.IsNotExist(err) {
			log.Printf("Warning: GeoJSON file not found for %s: %s", county, addressFile)
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
			log.Printf("Loaded 0 records from %s (no features in file)", strings.Title(county))
		}
	}
	
	log.Printf("Completed loading Ohio address data: %d records from %d counties", totalRecords, successfulCounties)
	return nil
}

// loadCountyAddresses loads address data from a single county GeoJSON file
func loadCountyAddresses(county, filePath string) (int, error) {
	// Open and read the GeoJSON file
	file, err := os.Open(filePath)
	if err != nil {
		return 0, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Parse GeoJSON
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

	if len(geoJSON.Features) == 0 {
		return 0, nil
	}

	// Prepare batch insert
	stmt, err := database.DB.Prepare(`
		INSERT INTO ohio_addresses (
			county, street_number, street_name, city, state, zip_code, 
			latitude, longitude, full_address, properties
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (county, latitude, longitude, full_address) DO NOTHING
	`)
	if err != nil {
		return 0, fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	insertedCount := 0

	for _, feature := range geoJSON.Features {
		if feature.Geometry.Type != "Point" || len(feature.Geometry.Coordinates) < 2 {
			continue
		}

		// Extract properties
		props := feature.Properties
		
		// Get coordinates (GeoJSON is [longitude, latitude])
		longitude := feature.Geometry.Coordinates[0]
		latitude := feature.Geometry.Coordinates[1]

		// Extract address components with various possible field names
		streetNumber := getStringProperty(props, "HOUSENUM", "HouseNum", "house_number", "housenumber")
		streetName := getStringProperty(props, "ST_NAME", "StreetName", "street_name", "STREETNAME", "LSN")
		city := getStringProperty(props, "USPS_CITY", "City", "city", "CITY", "MUNI")
		state := getStringProperty(props, "STATE", "State", "state")
		zipCode := getStringProperty(props, "ZIPCODE", "ZipCode", "zip_code", "zip")

		// Build full address
		fullAddress := buildFullAddress(streetNumber, streetName, city, state, zipCode)
		if fullAddress == "" {
			continue
		}

		// Convert properties to JSON for storage
		propsJSON, err := json.Marshal(props)
		if err != nil {
			log.Printf("Warning: Failed to marshal properties: %v", err)
			propsJSON = []byte("{}")
		}

		// Insert record
		_, err = stmt.Exec(
			strings.Title(county),
			streetNumber,
			streetName,
			city,
			state,
			zipCode,
			latitude,
			longitude,
			fullAddress,
			string(propsJSON),
		)
		if err != nil {
			log.Printf("Warning: Failed to insert record for %s: %v", fullAddress, err)
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

// buildFullAddress constructs a full address string from components
func buildFullAddress(streetNumber, streetName, city, state, zipCode string) string {
	parts := []string{}
	
	if streetNumber != "" && streetName != "" {
		parts = append(parts, streetNumber+" "+streetName)
	} else if streetName != "" {
		parts = append(parts, streetName)
	}
	
	if city != "" {
		parts = append(parts, city)
	}
	
	if state != "" {
		parts = append(parts, state)
	}
	
	if zipCode != "" {
		parts = append(parts, zipCode)
	}
	
	return strings.Join(parts, ", ")
}
