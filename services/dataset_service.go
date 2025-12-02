package services

import (
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"geocoding-api/database"
	"geocoding-api/models"
)

// DatasetService handles dataset operations
type DatasetService struct {
	db *sql.DB
}

// NewDatasetService creates a new DatasetService
func NewDatasetService(db *sql.DB) *DatasetService {
	return &DatasetService{db: db}
}

// UploadDirectory is where uploaded files are stored
const UploadDirectory = "./uploads"

// EnsureUploadDirectory creates the upload directory if it doesn't exist
func EnsureUploadDirectory() error {
	return os.MkdirAll(UploadDirectory, 0755)
}

// CreateDataset creates a new dataset record
func (s *DatasetService) CreateDataset(dataset *models.Dataset) error {
	query := `
		INSERT INTO datasets (name, state, county, file_type, file_path, file_size, 
			record_count, status, uploaded_by, uploaded_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, created_at, updated_at
	`

	return s.db.QueryRow(
		query,
		dataset.Name,
		dataset.State,
		dataset.County,
		dataset.FileType,
		dataset.FilePath,
		dataset.FileSize,
		dataset.RecordCount,
		dataset.Status,
		dataset.UploadedBy,
		dataset.UploadedAt,
	).Scan(&dataset.ID, &dataset.UploadedAt, &dataset.UploadedAt)
}

// GetDatasets retrieves all datasets with optional filtering
func (s *DatasetService) GetDatasets(state, status string, limit, offset int) ([]models.Dataset, int, error) {
	// Build query with filters
	whereConditions := []string{}
	args := []interface{}{}
	argCount := 1

	if state != "" {
		whereConditions = append(whereConditions, fmt.Sprintf("state = $%d", argCount))
		args = append(args, state)
		argCount++
	}

	if status != "" {
		whereConditions = append(whereConditions, fmt.Sprintf("status = $%d", argCount))
		args = append(args, status)
		argCount++
	}

	whereClause := ""
	if len(whereConditions) > 0 {
		whereClause = "WHERE " + strings.Join(whereConditions, " AND ")
	}

	// Get total count
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM datasets %s", whereClause)
	var total int
	if err := s.db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Get datasets
	query := fmt.Sprintf(`
		SELECT id, name, state, county, file_type, file_path, file_size, 
			record_count, status, error_message, uploaded_by, uploaded_at, processed_at
		FROM datasets
		%s
		ORDER BY uploaded_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argCount, argCount+1)

	args = append(args, limit, offset)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	datasets := []models.Dataset{}
	for rows.Next() {
		var dataset models.Dataset
		var errorMessage sql.NullString
		var processedAt sql.NullTime

		if err := rows.Scan(
			&dataset.ID,
			&dataset.Name,
			&dataset.State,
			&dataset.County,
			&dataset.FileType,
			&dataset.FilePath,
			&dataset.FileSize,
			&dataset.RecordCount,
			&dataset.Status,
			&errorMessage,
			&dataset.UploadedBy,
			&dataset.UploadedAt,
			&processedAt,
		); err != nil {
			return nil, 0, err
		}

		if errorMessage.Valid {
			dataset.ErrorMessage = errorMessage.String
		}
		if processedAt.Valid {
			dataset.ProcessedAt = &processedAt.Time
		}

		datasets = append(datasets, dataset)
	}

	return datasets, total, nil
}

// GetDatasetByID retrieves a dataset by ID
func (s *DatasetService) GetDatasetByID(id int) (*models.Dataset, error) {
	query := `
		SELECT id, name, state, county, file_type, file_path, file_size, 
			record_count, status, error_message, uploaded_by, uploaded_at, processed_at
		FROM datasets
		WHERE id = $1
	`

	var dataset models.Dataset
	var errorMessage sql.NullString
	var processedAt sql.NullTime

	err := s.db.QueryRow(query, id).Scan(
		&dataset.ID,
		&dataset.Name,
		&dataset.State,
		&dataset.County,
		&dataset.FileType,
		&dataset.FilePath,
		&dataset.FileSize,
		&dataset.RecordCount,
		&dataset.Status,
		&errorMessage,
		&dataset.UploadedBy,
		&dataset.UploadedAt,
		&processedAt,
	)

	if err != nil {
		return nil, err
	}

	if errorMessage.Valid {
		dataset.ErrorMessage = errorMessage.String
	}
	if processedAt.Valid {
		dataset.ProcessedAt = &processedAt.Time
	}

	return &dataset, nil
}

// UpdateDatasetStatus updates the status of a dataset
func (s *DatasetService) UpdateDatasetStatus(id int, status, errorMessage string, recordCount int) error {
	now := time.Now()
	query := `
		UPDATE datasets
		SET status = $1, error_message = $2, record_count = $3, processed_at = $4, updated_at = $5
		WHERE id = $6
	`

	_, err := s.db.Exec(query, status, errorMessage, recordCount, now, now, id)
	return err
}

// DeleteDataset deletes a dataset and its file
func (s *DatasetService) DeleteDataset(id int) error {
	// Get dataset to find file path
	dataset, err := s.GetDatasetByID(id)
	if err != nil {
		return err
	}

	// Delete file if it exists
	if dataset.FilePath != "" {
		if err := os.Remove(dataset.FilePath); err != nil && !os.IsNotExist(err) {
			log.Printf("Warning: Failed to delete file %s: %v", dataset.FilePath, err)
		}
	}

	// Delete database record
	_, err = s.db.Exec("DELETE FROM datasets WHERE id = $1", id)
	return err
}

// GetDatasetStats returns statistics about datasets
func (s *DatasetService) GetDatasetStats() (*models.DatasetStats, error) {
	stats := &models.DatasetStats{
		StateBreakdown:  make(map[string]int),
		StatusBreakdown: make(map[string]int),
	}

	// Get total datasets and records
	err := s.db.QueryRow(`
		SELECT 
			COUNT(*), 
			COALESCE(SUM(record_count), 0),
			COALESCE(SUM(file_size), 0)
		FROM datasets
	`).Scan(&stats.TotalDatasets, &stats.TotalRecords, &stats.TotalStorageSize)

	if err != nil {
		return nil, err
	}

	// Get state breakdown
	rows, err := s.db.Query(`
		SELECT state, COUNT(*) 
		FROM datasets 
		GROUP BY state 
		ORDER BY COUNT(*) DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var state string
		var count int
		if err := rows.Scan(&state, &count); err != nil {
			return nil, err
		}
		stats.StateBreakdown[state] = count
	}

	// Get status breakdown
	rows, err = s.db.Query(`
		SELECT status, COUNT(*) 
		FROM datasets 
		GROUP BY status
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		stats.StatusBreakdown[status] = count
	}

	return stats, nil
}

// ProcessGeoJSONDataset processes an uploaded GeoJSON file and imports addresses
func (s *DatasetService) ProcessGeoJSONDataset(datasetID int) error {
	dataset, err := s.GetDatasetByID(datasetID)
	if err != nil {
		return fmt.Errorf("failed to get dataset: %w", err)
	}

	// Update status to processing
	if err := s.UpdateDatasetStatus(datasetID, "processing", "", 0); err != nil {
		return fmt.Errorf("failed to update status: %w", err)
	}

	// Open file (handle both .gz and plain files)
	file, err := os.Open(dataset.FilePath)
	if err != nil {
		s.UpdateDatasetStatus(datasetID, "failed", err.Error(), 0)
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	var reader io.Reader = file

	// If file is gzipped, decompress it
	if strings.HasSuffix(dataset.FilePath, ".gz") {
		gzReader, err := gzip.NewReader(file)
		if err != nil {
			s.UpdateDatasetStatus(datasetID, "failed", err.Error(), 0)
			return fmt.Errorf("failed to create gzip reader: %w", err)
		}
		defer gzReader.Close()
		reader = gzReader
	}

	// Parse GeoJSON
	var geojson struct {
		Type     string `json:"type"`
		Features []struct {
			Type       string `json:"type"`
			Properties map[string]interface{} `json:"properties"`
			Geometry   struct {
				Type        string    `json:"type"`
				Coordinates []float64 `json:"coordinates"`
			} `json:"geometry"`
		} `json:"features"`
	}

	if err := json.NewDecoder(reader).Decode(&geojson); err != nil {
		s.UpdateDatasetStatus(datasetID, "failed", err.Error(), 0)
		return fmt.Errorf("failed to parse GeoJSON: %w", err)
	}

	// Process features and insert into database
	recordCount := 0
	skippedDuplicates := 0
	
	for _, feature := range geojson.Features {
		if feature.Geometry.Type != "Point" {
			continue
		}

		// Extract address components from properties
		// Supports multiple property naming conventions:
		// - Ohio LBRS format (HOUSENUM, ST_NAME, USPS_CITY, ZIPCODE)
		// - Generic format (HOUSE_NUMB, STREET, CITY, ZIP)
		// - Lowercase format (house_number, street, city, postcode)
		props := feature.Properties
		
		address := models.OhioAddress{
			Longitude: feature.Geometry.Coordinates[0],
			Latitude:  feature.Geometry.Coordinates[1],
		}

		// House Number - try multiple field names and types
		address.HouseNumber = getStringProp(props, "HOUSENUM", "HOUSE_NUMB", "house_number", "LHN")
		
		// Street Name - Ohio LBRS uses ST_NAME or LSN (full street with number)
		address.Street = getStringProp(props, "ST_NAME", "STREET", "street")
		if address.Street == "" {
			// Try LSN but remove the house number prefix
			if lsn := getStringProp(props, "LSN"); lsn != "" && address.HouseNumber != "" {
				// LSN format is "16551 STATE RTE 247" - remove the number prefix
				address.Street = strings.TrimSpace(strings.TrimPrefix(lsn, address.HouseNumber))
			}
		}
		
		// City - USPS_CITY or MUNI for Ohio LBRS
		address.City = getStringProp(props, "USPS_CITY", "CITY", "city", "MUNI", "COMM")
		
		// ZIP Code
		address.Postcode = getStringProp(props, "ZIPCODE", "ZIP", "postcode", "postal_code")
		
		// Unit/Apartment
		address.Unit = getStringProp(props, "UNITNUM", "UNIT", "unit", "UNITEXTRA")
		
		// District (county abbreviation like "ADA")
		address.District = getStringProp(props, "COUNTY", "district")

		// Set county and state from dataset metadata (full names)
		address.County = dataset.County
		address.Region = dataset.State

		// Insert address into database (using existing service)
		if address.HouseNumber != "" && address.Street != "" {
			// Use the existing address service to insert
			addressService := NewAddressService(database.DB)
			if _, err := addressService.CreateAddress(&address); err != nil {
				// Check if it's a duplicate (unique constraint violation)
				if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint") {
					skippedDuplicates++
				} else {
					log.Printf("Warning: Failed to insert address: %v", err)
				}
				continue
			}
			recordCount++
		}
	}

	// Update dataset status to completed
	if err := s.UpdateDatasetStatus(datasetID, "completed", "", recordCount); err != nil {
		return fmt.Errorf("failed to update completion status: %w", err)
	}

	// Delete the uploaded file after successful processing to save disk space
	if err := s.cleanupUploadedFile(dataset.FilePath); err != nil {
		log.Printf("Warning: Failed to cleanup uploaded file: %v", err)
		// Don't fail the operation, data is already imported
	}

	log.Printf("Successfully processed dataset %d: %d records imported, %d duplicates skipped", datasetID, recordCount, skippedDuplicates)
	return nil
}

// cleanupUploadedFile removes the uploaded file after processing
func (s *DatasetService) cleanupUploadedFile(filePath string) error {
	if filePath == "" {
		return nil
	}
	
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete file %s: %w", filePath, err)
	}
	
	log.Printf("Cleaned up uploaded file: %s", filePath)
	return nil
}

// getStringProp extracts a string value from properties map, trying multiple field names
// Handles both string values and numeric values (converting them to strings)
func getStringProp(props map[string]interface{}, fieldNames ...string) string {
	for _, name := range fieldNames {
		if val, ok := props[name]; ok && val != nil {
			switch v := val.(type) {
			case string:
				return strings.TrimSpace(v)
			case float64:
				// Handle numeric values (JSON numbers are float64)
				if v == float64(int(v)) {
					return fmt.Sprintf("%d", int(v))
				}
				return fmt.Sprintf("%v", v)
			case int:
				return fmt.Sprintf("%d", v)
			default:
				return fmt.Sprintf("%v", v)
			}
		}
	}
	return ""
}

// CheckDatasetExists checks if a dataset with the same state and county already exists
func (s *DatasetService) CheckDatasetExists(state, county string) (bool, *models.Dataset, error) {
	query := `
		SELECT id, name, state, county, status, record_count, uploaded_at
		FROM datasets
		WHERE UPPER(state) = UPPER($1) AND UPPER(county) = UPPER($2)
		ORDER BY uploaded_at DESC
		LIMIT 1
	`
	
	var dataset models.Dataset
	err := s.db.QueryRow(query, state, county).Scan(
		&dataset.ID,
		&dataset.Name,
		&dataset.State,
		&dataset.County,
		&dataset.Status,
		&dataset.RecordCount,
		&dataset.UploadedAt,
	)
	
	if err == sql.ErrNoRows {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, err
	}
	
	return true, &dataset, nil
}
