package services

import (
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"geocoding-api/database"
	"io"
	"log"
	"os"
	"strings"
	"time"

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

// geoFeature is a single GeoJSON feature. The streaming decoder holds exactly
// one of these at a time, which is the whole point: the previous
// implementation decoded the entire FeatureCollection into one slice, and the
// upload path accepts bodies up to 500MB.
type geoFeature struct {
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties"`
	Geometry   struct {
		Type        string    `json:"type"`
		Coordinates []float64 `json:"coordinates"`
	} `json:"geometry"`
}

// addressInsertBatchSize is how many addresses are sent per INSERT. Each row
// binds 11 parameters, so 1000 rows is 11,000 of PostgreSQL's 65,535 parameter
// limit -- comfortable headroom, and large enough that the round trip stops
// being the bottleneck.
const addressInsertBatchSize = 1000

// maxConsecutiveFailedBatches is how many entirely-failed batches are tolerated
// before the import gives up. One can be a bad chunk of a file; three in a row
// is the database, and pressing on would delete the source file for nothing.
const maxConsecutiveFailedBatches = 3

// errImportAborted marks a failure that came from writing rows rather than
// from reading the file, so the two can be told apart after they have both
// travelled back through streamGeoJSONFeatures.
var errImportAborted = errors.New("address import aborted")

// addressProgressInterval is how many imported rows pass between progress
// writes to the datasets row. A large county file is now processed in a single
// pass with no other operator visibility, so record_count is kept live.
const addressProgressInterval = 25000

// ProcessGeoJSONDataset processes an uploaded GeoJSON file and imports addresses.
//
// The file is streamed feature by feature rather than decoded whole, and rows
// are inserted in batches. A 500k-address county extract used to mean 500k
// round trips and a 500k-element slice resident in memory.
func (s *DatasetService) ProcessGeoJSONDataset(datasetID int) error {
	// The batched insert below uses ON CONFLICT (hash, region), whose index
	// migration 23 creates. Migrations run asynchronously, so without this a
	// deploy-window import fails every batch and marks the dataset failed --
	// indistinguishable from a corrupt file.
	if err := database.RequireSchemaVersion(s.db, database.SchemaVersionRegionUniqueness); err != nil {
		return fmt.Errorf("cannot import addresses yet: %w", err)
	}

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

	importer := newAddressImporter(s.db)

	// Stream the features array. Any parse failure part way through still
	// flushes what was already parsed -- those features were read correctly and
	// the old per-row implementation would have committed them -- and then
	// marks the dataset failed.
	streamErr := streamGeoJSONFeatures(reader, func(feature geoFeature) error {
		address, ok := addressFromFeature(feature, dataset.County, dataset.State)
		if !ok {
			return nil
		}
		if err := importer.add(address); err != nil {
			return err
		}
		if importer.imported >= importer.lastProgress+addressProgressInterval {
			importer.lastProgress = importer.imported
			if err := s.updateDatasetProgress(datasetID, importer.imported); err != nil {
				log.Printf("Warning: failed to update progress for dataset %d: %v", datasetID, err)
			}
		}
		return nil
	})

	// Flush whatever is pending regardless of how the stream ended.
	flushErr := importer.flush()

	if streamErr != nil {
		s.UpdateDatasetStatus(datasetID, "failed", streamErr.Error(), importer.imported)
		// streamGeoJSONFeatures surfaces two different failures through this
		// one return: a malformed file, and an error from the callback, which
		// here means the inserts gave up. Reporting an insert failure as
		// "failed to parse GeoJSON" sends whoever reads it to look at the file
		// instead of at the database.
		if errors.Is(streamErr, errImportAborted) {
			return fmt.Errorf("failed to insert addresses: %w", streamErr)
		}
		return fmt.Errorf("failed to parse GeoJSON: %w", streamErr)
	}
	if flushErr != nil {
		s.UpdateDatasetStatus(datasetID, "failed", flushErr.Error(), importer.imported)
		return fmt.Errorf("failed to insert addresses: %w", flushErr)
	}

	recordCount := importer.imported
	skippedDuplicates := importer.skipped

	// Rows the database rejected. The import did not abort -- enough batches
	// succeeded for that -- but it is not a clean run either, and the operator
	// needs to be told rather than left reading a green status.
	statusNote := ""
	if importer.failed > 0 {
		statusNote = fmt.Sprintf("%d address(es) could not be inserted; see server logs", importer.failed)
		log.Printf("Dataset %d completed with %d failed row(s)", datasetID, importer.failed)
	}

	if err := s.UpdateDatasetStatus(datasetID, "completed", statusNote, recordCount); err != nil {
		return fmt.Errorf("failed to update completion status: %w", err)
	}

	// Delete the uploaded file only on a clean run. Deleting it after a
	// partial import destroys the only copy of the rows that did not make it,
	// leaving no way to retry -- the disk saving is not worth that.
	if importer.failed == 0 {
		if err := s.cleanupUploadedFile(dataset.FilePath); err != nil {
			log.Printf("Warning: Failed to cleanup uploaded file: %v", err)
			// Don't fail the operation, data is already imported
		}
	} else {
		log.Printf("Keeping %s: %d row(s) failed and the file is the only way to retry them",
			dataset.FilePath, importer.failed)
	}

	// An import is the only thing that changes coverage, so drop the cached
	// snapshot instead of serving a stale one for the rest of the TTL.
	ResetCoverageCache()

	log.Printf("Successfully processed dataset %d: %d records imported, %d duplicates skipped, %d failed",
		datasetID, recordCount, skippedDuplicates, importer.failed)
	return nil
}

// updateDatasetProgress bumps the running record count without touching status
// or processed_at, which only the terminal transitions should set.
func (s *DatasetService) updateDatasetProgress(datasetID, recordCount int) error {
	_, err := s.db.Exec(
		`UPDATE datasets SET record_count = $1, updated_at = $2 WHERE id = $3`,
		recordCount, time.Now(), datasetID,
	)
	return err
}

// streamGeoJSONFeatures walks a FeatureCollection and hands each feature to fn
// without ever holding more than one feature in memory. Top level members
// other than "features" (type, crs, bbox) are skipped; they are small by
// definition.
func streamGeoJSONFeatures(reader io.Reader, fn func(geoFeature) error) error {
	dec := json.NewDecoder(reader)

	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("failed to read GeoJSON: %w", err)
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return fmt.Errorf("expected a GeoJSON object, got %v", tok)
	}

	sawFeatures := false
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("failed to read GeoJSON key: %w", err)
		}
		key, ok := keyTok.(string)
		if !ok {
			return fmt.Errorf("expected a GeoJSON member name, got %v", keyTok)
		}

		if key != "features" {
			// Skip this member's value without interpreting it.
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return fmt.Errorf("failed to skip GeoJSON member %q: %w", key, err)
			}
			continue
		}

		sawFeatures = true
		tok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("failed to read features array: %w", err)
		}
		if delim, ok := tok.(json.Delim); !ok || delim != '[' {
			return fmt.Errorf("expected features to be an array, got %v", tok)
		}

		for dec.More() {
			var feature geoFeature
			if err := dec.Decode(&feature); err != nil {
				return fmt.Errorf("failed to parse feature: %w", err)
			}
			if err := fn(feature); err != nil {
				return err
			}
		}

		// Consume the closing ']'.
		if _, err := dec.Token(); err != nil {
			return fmt.Errorf("failed to close features array: %w", err)
		}
	}

	if !sawFeatures {
		return fmt.Errorf("GeoJSON has no features array")
	}
	return nil
}

// addressFromFeature maps one feature onto an address row, returning false when
// the feature should be skipped. The property name fallbacks cover the formats
// this API ingests: Ohio LBRS (HOUSENUM, ST_NAME, USPS_CITY, ZIPCODE), a
// generic uppercase format, and lowercase OpenAddresses-style keys.
func addressFromFeature(feature geoFeature, county, state string) (models.OhioAddress, bool) {
	var address models.OhioAddress

	if feature.Geometry.Type != "Point" || len(feature.Geometry.Coordinates) < 2 {
		return address, false
	}

	props := feature.Properties
	address.Longitude = feature.Geometry.Coordinates[0]
	address.Latitude = feature.Geometry.Coordinates[1]

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
	address.County = county
	// Upper-cased because the uniqueness key is (hash, region): 'oh' and 'OH'
	// are distinct values to the index, so a lowercase upload would reintroduce
	// the cross-state duplicates migration 23 closed.
	address.Region = strings.ToUpper(strings.TrimSpace(state))

	if address.HouseNumber == "" || address.Street == "" {
		return address, false
	}
	return address, true
}

// addressHash is the deduplication key for ohio_addresses.hash. It must stay
// byte-identical to the one AddressService.CreateAddress builds, or the same
// address imported by the two paths would land twice.
func addressHash(a models.OhioAddress) string {
	return fmt.Sprintf("%s|%s|%s|%s|%s", a.HouseNumber, a.Street, a.Unit, a.City, a.Postcode)
}

// addressImporter accumulates addresses and writes them in batches.
type addressImporter struct {
	db    *sql.DB
	batch []models.OhioAddress
	// seen holds the hashes in the pending batch. A file that repeats an
	// address within one batch must be counted as a duplicate, exactly as the
	// old row at a time path did, and deduplicating here also keeps a single
	// statement from conflicting with its own rows.
	seen     map[string]struct{}
	imported int
	skipped  int
	// failed counts rows the database rejected. Kept apart from skipped
	// because they mean opposite things: a skipped row is an address already
	// present, a failed row is one that was supposed to land and did not.
	// Folding them together reported a broken import as a clean one full of
	// duplicates.
	failed int
	// consecutiveFailedBatches drives the abort in flush.
	consecutiveFailedBatches int
	lastProgress             int
}

func newAddressImporter(db *sql.DB) *addressImporter {
	return &addressImporter{
		db:    db,
		batch: make([]models.OhioAddress, 0, addressInsertBatchSize),
		seen:  make(map[string]struct{}, addressInsertBatchSize),
	}
}

func (ai *addressImporter) add(address models.OhioAddress) error {
	hash := addressHash(address)
	if _, dup := ai.seen[hash]; dup {
		ai.skipped++
		return nil
	}
	ai.seen[hash] = struct{}{}
	ai.batch = append(ai.batch, address)

	if len(ai.batch) >= addressInsertBatchSize {
		return ai.flush()
	}
	return nil
}

// flush writes the pending batch. Each batch is a single statement, which
// PostgreSQL already runs in its own implicit transaction, so an explicit BEGIN
// would add a round trip and buy nothing. Batches commit independently on
// purpose: a failure at row 499,000 of a 500,000 row import should not discard
// everything that came before it, which matches how the old per-row loop behaved.
func (ai *addressImporter) flush() error {
	if len(ai.batch) == 0 {
		return nil
	}
	batch := ai.batch
	ai.batch = make([]models.OhioAddress, 0, addressInsertBatchSize)
	ai.seen = make(map[string]struct{}, addressInsertBatchSize)

	inserted, err := ai.insertBatch(batch)
	rowFailures := 0
	if err != nil {
		// One malformed row must not cost the other 999. The old loop logged
		// and continued past a bad insert, so fall back to row at a time and
		// preserve that.
		log.Printf("Warning: batch insert of %d addresses failed (%v), retrying individually", len(batch), err)
		inserted = 0
		var lastErr error
		for i := range batch {
			n, rowErr := ai.insertBatch(batch[i : i+1])
			if rowErr != nil {
				rowFailures++
				lastErr = rowErr
				log.Printf("Warning: Failed to insert address: %v", rowErr)
				continue
			}
			inserted += n
		}

		// A batch where every single row also failed on its own is not bad
		// data, it is a broken database: the connection dropped, or a
		// constraint is rejecting everything. Continuing means marking the
		// dataset completed and deleting the operator's uploaded file while
		// importing nothing, so give up after a few in a row.
		if rowFailures == len(batch) {
			ai.consecutiveFailedBatches++
			if ai.consecutiveFailedBatches >= maxConsecutiveFailedBatches {
				return fmt.Errorf("%w: %d consecutive batches failed entirely, last error: %v",
					errImportAborted, ai.consecutiveFailedBatches, lastErr)
			}
		} else {
			ai.consecutiveFailedBatches = 0
		}
	} else {
		ai.consecutiveFailedBatches = 0
	}

	ai.imported += inserted
	ai.failed += rowFailures
	// Whatever is left neither inserted nor errored was rejected by
	// ON CONFLICT, which is the definition of a duplicate.
	ai.skipped += len(batch) - inserted - rowFailures
	return nil
}

// insertBatch inserts rows in one multi-value statement, skipping any address
// whose hash is already present. ON CONFLICT is why this is a plain INSERT and
// not COPY: COPY cannot express it, and the duplicate skipping is load bearing
// because these county extracts overlap.
func (ai *addressImporter) insertBatch(batch []models.OhioAddress) (int, error) {
	if len(batch) == 0 {
		return 0, nil
	}

	const columnsPerRow = 11
	values := make([]string, 0, len(batch))
	args := make([]interface{}, 0, len(batch)*columnsPerRow)

	for i, a := range batch {
		base := i * columnsPerRow
		values = append(values, fmt.Sprintf(
			"($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, ST_SetSRID(ST_MakePoint($%d, $%d), 4326))",
			base+1, base+2, base+3, base+4, base+5, base+6, base+7, base+8, base+9, base+10, base+11,
		))
		args = append(args,
			addressHash(a),
			a.HouseNumber,
			a.Street,
			a.Unit,
			a.City,
			a.District,
			a.Region,
			a.Postcode,
			a.County,
			a.Longitude,
			a.Latitude,
		)
	}

	query := `
		INSERT INTO ohio_addresses (
			hash, house_number, street, unit, city, district, region, postcode, county, geom
		) VALUES ` + strings.Join(values, ", ") + `
		ON CONFLICT (hash, region) DO NOTHING
	`

	result, err := ai.db.Exec(query, args...)
	if err != nil {
		return 0, err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		// Should not happen with lib/pq, which parses the INSERT command tag.
		return 0, fmt.Errorf("failed to read inserted row count: %w", err)
	}
	return int(affected), nil
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
