package handlers

import (
	"net/http"
	"strconv"

	"geocoding-api/services"

	"github.com/labstack/echo/v4"
)

// GeocodeResponse represents the standard API response structure
type GeocodeResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
	Count   int         `json:"count,omitempty"`
}

// GetZipCodeHandler handles GET requests for ZIP code lookup
func GetZipCodeHandler(c echo.Context) error {
	zipCode := c.Param("zipcode")
	if zipCode == "" {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{
			Success: false,
			Error:   "ZIP code parameter is required",
		})
	}

	// Validate ZIP code format (basic validation)
	if len(zipCode) < 5 || len(zipCode) > 10 {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{
			Success: false,
			Error:   "Invalid ZIP code format",
		})
	}

	result, err := services.GetZipCodeByZip(zipCode)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, GeocodeResponse{
			Success: false,
			Error:   "Failed to retrieve ZIP code data",
		})
	}

	if result == nil {
		return c.JSON(http.StatusNotFound, GeocodeResponse{
			Success: false,
			Error:   "ZIP code not found",
		})
	}

	return c.JSON(http.StatusOK, GeocodeResponse{
		Success: true,
		Data:    result,
		Count:   1,
	})
}

// SearchZipCodesHandler handles GET requests for ZIP code search by city
func SearchZipCodesHandler(c echo.Context) error {
	cityName := c.QueryParam("city")
	if cityName == "" {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{
			Success: false,
			Error:   "City parameter is required",
		})
	}

	stateCode := c.QueryParam("state")
	limitStr := c.QueryParam("limit")
	
	// Default limit is 50, max is 100
	limit := 50
	if limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil {
			if parsedLimit > 0 && parsedLimit <= 100 {
				limit = parsedLimit
			}
		}
	}

	results, err := services.SearchZipCodesByCity(cityName, stateCode, limit)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, GeocodeResponse{
			Success: false,
			Error:   "Failed to search ZIP codes",
		})
	}

	return c.JSON(http.StatusOK, GeocodeResponse{
		Success: true,
		Data:    results,
		Count:   len(results),
	})
}

// HealthCheckHandler handles health check requests
func HealthCheckHandler(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]interface{}{
		"status":        "healthy",
		"service":       "geocoding-api",
		"version":       "1.0.0",
		"documentation": "http://localhost:8080/docs",
		"openapi_spec":  "http://localhost:8080/api-docs.yaml",
	})
}

// DocsRedirectHandler redirects root requests to documentation
func DocsRedirectHandler(c echo.Context) error {
	return c.Redirect(http.StatusPermanentRedirect, "/docs")
}



// LoadDataHandler handles POST requests to load CSV data (admin endpoint)
func LoadDataHandler(c echo.Context) error {
	// In a production environment, you might want to add authentication here
	
	filePath := c.QueryParam("file")
	if filePath == "" {
		filePath = "georef-united-states-of-america-zc-point.csv" // Default file
	}

	err := services.LoadZipCodesFromCSV(filePath)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, GeocodeResponse{
			Success: false,
			Error:   "Failed to load CSV data: " + err.Error(),
		})
	}

	return c.JSON(http.StatusOK, GeocodeResponse{
		Success: true,
		Data:    "CSV data loaded successfully",
	})
}