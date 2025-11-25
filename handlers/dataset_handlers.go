package handlers

import (
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"geocoding-api/models"
	"geocoding-api/services"

	"github.com/labstack/echo/v4"
)

// UploadDatasetHandler handles single file uploads for county address data
func UploadDatasetHandler(c echo.Context) error {
	// Get form values
	name := c.FormValue("name")
	state := c.FormValue("state")
	county := c.FormValue("county")

	if name == "" || state == "" || county == "" {
		return c.JSON(http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "name, state, and county are required",
		})
	}

	// Get uploaded file
	file, err := c.FormFile("file")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "file is required",
		})
	}

	// Get user ID from context
	userID, ok := c.Get("user_id").(int)
	if !ok {
		return c.JSON(http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   "failed to get user ID",
		})
	}

	// Save and create dataset
	dataset, err := saveUploadedFile(file, name, state, county, userID)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
	}

	// Process the dataset asynchronously
	go func() {
		datasetService := services.NewDatasetService(services.GetDB())
		if err := datasetService.ProcessGeoJSONDataset(dataset.ID); err != nil {
			fmt.Printf("Error processing dataset %d: %v\n", dataset.ID, err)
		}
	}()

	return c.JSON(http.StatusOK, map[string]interface{}{
		"success": true,
		"data":    dataset,
		"message": "File uploaded successfully and processing started",
	})
}

// BatchUploadResult represents the result of uploading a single file in a batch
type BatchUploadResult struct {
	Filename string          `json:"filename"`
	Success  bool            `json:"success"`
	Error    string          `json:"error,omitempty"`
	Dataset  *models.Dataset `json:"dataset,omitempty"`
}

// UploadMultipleHandler handles multiple file uploads with concurrent processing
func UploadMultipleHandler(c echo.Context) error {
	// Recover from any panics
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("[BulkUpload] PANIC recovered: %v\n", r)
			fmt.Printf("[BulkUpload] Stack trace:\n%s\n", debug.Stack())
		}
	}()

	fmt.Println("[BulkUpload] Starting bulk upload request")
	
	// Get form values
	state := c.FormValue("state")
	fmt.Printf("[BulkUpload] State: %s\n", state)

	if state == "" {
		fmt.Println("[BulkUpload] ERROR: state is required")
		return c.JSON(http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "state is required",
		})
	}

	// Get user ID from context
	userID, ok := c.Get("user_id").(int)
	if !ok {
		return c.JSON(http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   "failed to get user ID",
		})
	}

	// Get the multipart form
	fmt.Println("[BulkUpload] Parsing multipart form...")
	form, err := c.MultipartForm()
	if err != nil {
		fmt.Printf("[BulkUpload] ERROR parsing form: %v\n", err)
		return c.JSON(http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "failed to parse multipart form: " + err.Error(),
		})
	}

	files := form.File["files"]
	fmt.Printf("[BulkUpload] Found %d files in form\n", len(files))
	if len(files) == 0 {
		fmt.Println("[BulkUpload] ERROR: no files provided")
		return c.JSON(http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "no files provided",
		})
	}
	
	// Log all file names
	for i, f := range files {
		fmt.Printf("[BulkUpload] File %d: %s (size: %d bytes)\n", i+1, f.Filename, f.Size)
	}

	// Ensure upload directory exists
	if err := services.EnsureUploadDirectory(); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   "failed to create upload directory",
		})
	}

	// Process files concurrently with a worker pool
	// Limit concurrency to avoid overwhelming the system
	maxWorkers := 4
	if len(files) < maxWorkers {
		maxWorkers = len(files)
	}

	jobs := make(chan *multipart.FileHeader, len(files))
	results := make(chan BatchUploadResult, len(files))

	// Start workers
	fmt.Printf("[BulkUpload] Starting %d workers...\n", maxWorkers)
	var wg sync.WaitGroup
	for i := 0; i < maxWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			fmt.Printf("[BulkUpload] Worker %d started\n", workerID)
			for file := range jobs {
				fmt.Printf("[BulkUpload] Worker %d processing: %s\n", workerID, file.Filename)
				result := processUploadedFile(file, state, userID)
				if result.Success {
					fmt.Printf("[BulkUpload] Worker %d SUCCESS: %s -> Dataset ID %d\n", workerID, file.Filename, result.Dataset.ID)
				} else {
					fmt.Printf("[BulkUpload] Worker %d FAILED: %s -> %s\n", workerID, file.Filename, result.Error)
				}
				results <- result
			}
			fmt.Printf("[BulkUpload] Worker %d finished\n", workerID)
		}(i)
	}

	// Send jobs to workers
	for _, file := range files {
		jobs <- file
	}
	close(jobs)

	// Wait for all workers to finish
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	fmt.Println("[BulkUpload] Collecting results...")
	uploadResults := make([]BatchUploadResult, 0, len(files))
	successCount := 0
	failCount := 0
	var datasetIDs []int

	for result := range results {
		uploadResults = append(uploadResults, result)
		if result.Success {
			successCount++
			if result.Dataset != nil {
				datasetIDs = append(datasetIDs, result.Dataset.ID)
			}
		} else {
			failCount++
		}
	}

	fmt.Printf("[BulkUpload] Upload complete: %d success, %d failed out of %d total\n", successCount, failCount, len(files))

	// Start concurrent processing for all successfully uploaded datasets
	if len(datasetIDs) > 0 {
		fmt.Printf("[BulkUpload] Starting background processing for %d datasets: %v\n", len(datasetIDs), datasetIDs)
		go processDatasetsConcurrently(datasetIDs)
	} else {
		fmt.Println("[BulkUpload] No datasets to process")
	}

	fmt.Println("[BulkUpload] Returning response to client")
	return c.JSON(http.StatusOK, map[string]interface{}{
		"success":       failCount == 0,
		"total_files":   len(files),
		"success_count": successCount,
		"fail_count":    failCount,
		"results":       uploadResults,
		"message":       fmt.Sprintf("Uploaded %d of %d files. Processing started.", successCount, len(files)),
	})
}

// processUploadedFile handles a single file upload in the batch
func processUploadedFile(file *multipart.FileHeader, state string, userID int) BatchUploadResult {
	filename := file.Filename
	fmt.Printf("[ProcessFile] Processing: %s\n", filename)
	
	// Extract county name from filename (e.g., "adams-addresses-county.geojson.gz" -> "Adams")
	county := extractCountyFromFilename(filename)
	if county == "" {
		fmt.Printf("[ProcessFile] ERROR: could not extract county from filename: %s\n", filename)
		return BatchUploadResult{
			Filename: filename,
			Success:  false,
			Error:    "could not extract county name from filename",
		}
	}
	fmt.Printf("[ProcessFile] Extracted county: %s from %s\n", county, filename)

	// Generate name from filename
	name := fmt.Sprintf("%s County Addresses", strings.Title(county))
	fmt.Printf("[ProcessFile] Saving file as: %s\n", name)

	dataset, err := saveUploadedFile(file, name, state, county, userID)
	if err != nil {
		fmt.Printf("[ProcessFile] ERROR saving file %s: %v\n", filename, err)
		return BatchUploadResult{
			Filename: filename,
			Success:  false,
			Error:    err.Error(),
		}
	}

	fmt.Printf("[ProcessFile] SUCCESS: %s saved as dataset ID %d\n", filename, dataset.ID)
	return BatchUploadResult{
		Filename: filename,
		Success:  true,
		Dataset:  dataset,
	}
}

// extractCountyFromFilename extracts county name from common filename patterns
func extractCountyFromFilename(filename string) string {
	// Remove extension(s)
	name := filename
	name = strings.TrimSuffix(name, ".gz")
	name = strings.TrimSuffix(name, ".geojson")
	name = strings.TrimSuffix(name, ".json")
	
	// Common patterns:
	// "adams-addresses-county" -> "adams"
	// "adams_addresses_county" -> "adams"
	// "adams-county" -> "adams"
	// "adams" -> "adams"
	
	// Try to extract county name
	parts := strings.FieldsFunc(name, func(r rune) bool {
		return r == '-' || r == '_'
	})
	
	if len(parts) > 0 {
		// Return first part, capitalized
		return strings.Title(strings.ToLower(parts[0]))
	}
	
	return ""
}

// saveUploadedFile saves a file and creates a dataset record
func saveUploadedFile(file *multipart.FileHeader, name, state, county string, userID int) (*models.Dataset, error) {
	fmt.Printf("[SaveFile] Starting save for: %s (state=%s, county=%s)\n", file.Filename, state, county)
	
	// Validate file type
	allowedExtensions := []string{".geojson", ".json", ".gz"}
	ext := strings.ToLower(filepath.Ext(file.Filename))
	isValid := false
	for _, allowed := range allowedExtensions {
		if ext == allowed || strings.HasSuffix(file.Filename, ".geojson.gz") {
			isValid = true
			break
		}
	}

	if !isValid {
		fmt.Printf("[SaveFile] ERROR: invalid file extension: %s\n", ext)
		return nil, fmt.Errorf("file must be .geojson, .json, or .geojson.gz")
	}

	// Ensure upload directory exists
	if err := services.EnsureUploadDirectory(); err != nil {
		fmt.Printf("[SaveFile] ERROR creating upload directory: %v\n", err)
		return nil, fmt.Errorf("failed to create upload directory: %w", err)
	}

	// Generate unique filename
	timestamp := time.Now().UnixNano()
	sanitizedName := strings.ReplaceAll(name, " ", "_")
	filename := fmt.Sprintf("%d_%s_%s_%s%s", timestamp, state, county, sanitizedName, filepath.Ext(file.Filename))
	destPath := filepath.Join(services.UploadDirectory, filename)
	fmt.Printf("[SaveFile] Destination path: %s\n", destPath)

	// Save file
	src, err := file.Open()
	if err != nil {
		fmt.Printf("[SaveFile] ERROR opening uploaded file: %v\n", err)
		return nil, fmt.Errorf("failed to open uploaded file: %w", err)
	}
	defer src.Close()

	dest, err := os.Create(destPath)
	if err != nil {
		fmt.Printf("[SaveFile] ERROR creating destination file: %v\n", err)
		return nil, fmt.Errorf("failed to create destination file: %w", err)
	}
	defer dest.Close()

	written, err := io.Copy(dest, src)
	if err != nil {
		os.Remove(destPath)
		fmt.Printf("[SaveFile] ERROR copying file: %v\n", err)
		return nil, fmt.Errorf("failed to save file: %w", err)
	}
	fmt.Printf("[SaveFile] Written %d bytes to %s\n", written, destPath)

	// Determine file type
	fileType := "geojson"
	if strings.Contains(file.Filename, ".json") && !strings.Contains(file.Filename, ".geojson") {
		fileType = "json"
	}

	// Create dataset record
	fmt.Printf("[SaveFile] Creating dataset record in database...\n")
	datasetService := services.NewDatasetService(services.GetDB())
	dataset := &models.Dataset{
		Name:        name,
		State:       strings.ToUpper(state),
		County:      strings.Title(strings.ToLower(county)),
		FileType:    fileType,
		FilePath:    destPath,
		FileSize:    written,
		RecordCount: 0,
		Status:      "pending",
		UploadedBy:  userID,
		UploadedAt:  time.Now(),
	}

	if err := datasetService.CreateDataset(dataset); err != nil {
		os.Remove(destPath)
		fmt.Printf("[SaveFile] ERROR creating dataset record: %v\n", err)
		return nil, fmt.Errorf("failed to create dataset record: %w", err)
	}

	fmt.Printf("[SaveFile] SUCCESS: Created dataset ID %d for %s\n", dataset.ID, file.Filename)
	return dataset, nil
}

// processDatasetsConcurrently processes multiple datasets using a worker pool
func processDatasetsConcurrently(datasetIDs []int) {
	// Use a worker pool with limited concurrency
	maxWorkers := 4
	if len(datasetIDs) < maxWorkers {
		maxWorkers = len(datasetIDs)
	}

	jobs := make(chan int, len(datasetIDs))
	var wg sync.WaitGroup

	// Start workers
	for i := 0; i < maxWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			datasetService := services.NewDatasetService(services.GetDB())
			
			for datasetID := range jobs {
				fmt.Printf("[Worker %d] Processing dataset %d\n", workerID, datasetID)
				if err := datasetService.ProcessGeoJSONDataset(datasetID); err != nil {
					fmt.Printf("[Worker %d] Error processing dataset %d: %v\n", workerID, datasetID, err)
				} else {
					fmt.Printf("[Worker %d] Completed dataset %d\n", workerID, datasetID)
				}
			}
		}(i)
	}

	// Send jobs
	for _, id := range datasetIDs {
		jobs <- id
	}
	close(jobs)

	// Wait for completion
	wg.Wait()
	fmt.Printf("All %d datasets processed\n", len(datasetIDs))
}

// GetDatasetsHandler lists all datasets with optional filtering
func GetDatasetsHandler(c echo.Context) error {
	state := c.QueryParam("state")
	status := c.QueryParam("status")
	
	limitStr := c.QueryParam("limit")
	limit := 50
	if limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	offsetStr := c.QueryParam("offset")
	offset := 0
	if offsetStr != "" {
		if parsed, err := strconv.Atoi(offsetStr); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	datasetService := services.NewDatasetService(services.GetDB())
	datasets, total, err := datasetService.GetDatasets(state, status, limit, offset)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   "failed to get datasets",
		})
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"datasets": datasets,
			"total":    total,
			"limit":    limit,
			"offset":   offset,
		},
	})
}

// GetDatasetHandler gets a single dataset by ID
func GetDatasetHandler(c echo.Context) error {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "invalid dataset ID",
		})
	}

	datasetService := services.NewDatasetService(services.GetDB())
	dataset, err := datasetService.GetDatasetByID(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]interface{}{
			"success": false,
			"error":   "dataset not found",
		})
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"success": true,
		"data":    dataset,
	})
}

// DeleteDatasetHandler deletes a dataset
func DeleteDatasetHandler(c echo.Context) error {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "invalid dataset ID",
		})
	}

	datasetService := services.NewDatasetService(services.GetDB())
	if err := datasetService.DeleteDataset(id); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   "failed to delete dataset",
		})
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "dataset deleted successfully",
	})
}

// ReprocessDatasetHandler reprocesses a failed dataset
func ReprocessDatasetHandler(c echo.Context) error {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "invalid dataset ID",
		})
	}

	datasetService := services.NewDatasetService(services.GetDB())
	dataset, err := datasetService.GetDatasetByID(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]interface{}{
			"success": false,
			"error":   "dataset not found",
		})
	}

	if dataset.Status == "processing" {
		return c.JSON(http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "dataset is already processing",
		})
	}

	// Process the dataset asynchronously
	go func() {
		if err := datasetService.ProcessGeoJSONDataset(id); err != nil {
			fmt.Printf("Error reprocessing dataset %d: %v\n", id, err)
		}
	}()

	return c.JSON(http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "dataset reprocessing started",
	})
}

// GetDatasetStatsHandler returns statistics about datasets
func GetDatasetStatsHandler(c echo.Context) error {
	datasetService := services.NewDatasetService(services.GetDB())
	stats, err := datasetService.GetDatasetStats()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   "failed to get dataset statistics",
		})
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"success": true,
		"data":    stats,
	})
}
