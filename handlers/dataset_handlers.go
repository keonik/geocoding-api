package handlers

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"geocoding-api/models"
	"geocoding-api/services"

	"github.com/labstack/echo/v4"
)

// UploadDatasetHandler handles file uploads for county address data
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
		return c.JSON(http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "file must be .geojson, .json, or .geojson.gz",
		})
	}

	// Ensure upload directory exists
	if err := services.EnsureUploadDirectory(); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   "failed to create upload directory",
		})
	}

	// Generate unique filename
	timestamp := time.Now().Unix()
	sanitizedName := strings.ReplaceAll(name, " ", "_")
	filename := fmt.Sprintf("%d_%s_%s_%s%s", timestamp, state, county, sanitizedName, filepath.Ext(file.Filename))
	destPath := filepath.Join(services.UploadDirectory, filename)

	// Save file
	src, err := file.Open()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   "failed to open uploaded file",
		})
	}
	defer src.Close()

	dest, err := os.Create(destPath)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   "failed to create destination file",
		})
	}
	defer dest.Close()

	written, err := io.Copy(dest, src)
	if err != nil {
		os.Remove(destPath) // Clean up on error
		return c.JSON(http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   "failed to save file",
		})
	}

	// Get user ID from context
	userID, ok := c.Get("user_id").(int)
	if !ok {
		os.Remove(destPath) // Clean up on error
		return c.JSON(http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   "failed to get user ID",
		})
	}

	// Determine file type
	fileType := "geojson"
	if strings.Contains(file.Filename, ".geojson") {
		fileType = "geojson"
	} else if strings.Contains(file.Filename, ".json") {
		fileType = "json"
	}

	// Create dataset record
	datasetService := services.NewDatasetService(services.GetDB())
	dataset := &models.Dataset{
		Name:        name,
		State:       state,
		County:      county,
		FileType:    fileType,
		FilePath:    destPath,
		FileSize:    written,
		RecordCount: 0,
		Status:      "pending",
		UploadedBy:  userID,
		UploadedAt:  time.Now(),
	}

	if err := datasetService.CreateDataset(dataset); err != nil {
		os.Remove(destPath) // Clean up on error
		return c.JSON(http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   "failed to create dataset record",
		})
	}

	// Process the dataset asynchronously
	go func() {
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
