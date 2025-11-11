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
	Message string      `json:"message,omitempty"`
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

// CalculateDistanceHandler handles GET requests to calculate distance between two ZIP codes
func CalculateDistanceHandler(c echo.Context) error {
	fromZip := c.Param("from")
	toZip := c.Param("to")

	if fromZip == "" || toZip == "" {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{
			Success: false,
			Error:   "Both 'from' and 'to' ZIP code parameters are required",
		})
	}

	// Validate ZIP code formats
	if len(fromZip) < 5 || len(fromZip) > 10 || len(toZip) < 5 || len(toZip) > 10 {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{
			Success: false,
			Error:   "Invalid ZIP code format",
		})
	}

	result, err := services.CalculateDistanceBetweenZipCodes(fromZip, toZip)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, GeocodeResponse{
			Success: false,
			Error:   "Failed to calculate distance: " + err.Error(),
		})
	}

	return c.JSON(http.StatusOK, GeocodeResponse{
		Success: true,
		Data:    result,
		Count:   1,
	})
}

// FindNearbyZipCodesHandler handles GET requests to find ZIP codes within a radius
func FindNearbyZipCodesHandler(c echo.Context) error {
	centerZip := c.Param("zipcode")
	if centerZip == "" {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{
			Success: false,
			Error:   "Center ZIP code parameter is required",
		})
	}

	// Validate ZIP code format
	if len(centerZip) < 5 || len(centerZip) > 10 {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{
			Success: false,
			Error:   "Invalid ZIP code format",
		})
	}

	// Parse radius parameter
	radiusStr := c.QueryParam("radius")
	if radiusStr == "" {
		radiusStr = "1" // Default to 1 mile
	}

	radius, err := strconv.ParseFloat(radiusStr, 64)
	if err != nil || radius <= 0 || radius > 100 {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{
			Success: false,
			Error:   "Invalid radius parameter (must be between 0 and 100 miles)",
		})
	}

	// Parse limit parameter
	limitStr := c.QueryParam("limit")
	limit := 50 // Default limit
	if limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil {
			if parsedLimit > 0 && parsedLimit <= 200 {
				limit = parsedLimit
			}
		}
	}

	results, err := services.FindZipCodesWithinRadius(centerZip, radius, limit)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, GeocodeResponse{
			Success: false,
			Error:   "Failed to find nearby ZIP codes: " + err.Error(),
		})
	}

	return c.JSON(http.StatusOK, GeocodeResponse{
		Success: true,
		Data:    results,
		Count:   len(results),
	})
}

// CheckZipCodeProximityHandler handles GET requests to check if two ZIP codes are within a specific radius
func CheckZipCodeProximityHandler(c echo.Context) error {
	centerZip := c.Param("center")
	targetZip := c.Param("target")

	if centerZip == "" || targetZip == "" {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{
			Success: false,
			Error:   "Both 'center' and 'target' ZIP code parameters are required",
		})
	}

	// Validate ZIP code formats
	if len(centerZip) < 5 || len(centerZip) > 10 || len(targetZip) < 5 || len(targetZip) > 10 {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{
			Success: false,
			Error:   "Invalid ZIP code format",
		})
	}

	// Parse radius parameter
	radiusStr := c.QueryParam("radius")
	if radiusStr == "" {
		radiusStr = "1" // Default to 1 mile
	}

	radius, err := strconv.ParseFloat(radiusStr, 64)
	if err != nil || radius <= 0 || radius > 100 {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{
			Success: false,
			Error:   "Invalid radius parameter (must be between 0 and 100 miles)",
		})
	}

	isWithin, actualDistance, err := services.IsZipCodeWithinRadius(centerZip, targetZip, radius)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, GeocodeResponse{
			Success: false,
			Error:   "Failed to check ZIP code proximity: " + err.Error(),
		})
	}

	result := map[string]interface{}{
		"center_zip_code":    centerZip,
		"target_zip_code":    targetZip,
		"radius_miles":       radius,
		"is_within_radius":   isWithin,
		"actual_distance_miles": actualDistance,
		"actual_distance_km":    actualDistance * 1.60934,
	}

	return c.JSON(http.StatusOK, GeocodeResponse{
		Success: true,
		Data:    result,
		Count:   1,
	})
}