package handlers

import (
	"net/http"
	"strconv"

	"geocoding-api/models"
	"geocoding-api/services"

	"github.com/labstack/echo/v4"
)

// GetCountiesHandler returns a list of all Ohio counties
func GetCountiesHandler(c echo.Context) error {
	params := models.CountySearchParams{
		Name:         c.QueryParam("name"),
		MinAddresses: 0,
		MaxAddresses: 0,
		Limit:        100, // Default limit
		Offset:       0,
	}

	// Parse numeric parameters
	if minAddr := c.QueryParam("min_addresses"); minAddr != "" {
		if val, err := strconv.Atoi(minAddr); err == nil {
			params.MinAddresses = val
		}
	}

	if maxAddr := c.QueryParam("max_addresses"); maxAddr != "" {
		if val, err := strconv.Atoi(maxAddr); err == nil {
			params.MaxAddresses = val
		}
	}

	if limit := c.QueryParam("limit"); limit != "" {
		if val, err := strconv.Atoi(limit); err == nil && val > 0 && val <= 1000 {
			params.Limit = val
		}
	}

	if offset := c.QueryParam("offset"); offset != "" {
		if val, err := strconv.Atoi(offset); err == nil && val >= 0 {
			params.Offset = val
		}
	}

	counties, err := services.County.GetAllCounties(params)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   "Failed to fetch counties: " + err.Error(),
		})
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"success": true,
		"data":    counties,
		"count":   len(counties),
	})
}

// GetCountyDetailHandler returns detailed information about a specific county
func GetCountyDetailHandler(c echo.Context) error {
	countyName := c.Param("name")
	if countyName == "" {
		return c.JSON(http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "County name is required",
		})
	}

	county, err := services.County.GetCountyByName(countyName)
	if err != nil {
		if err.Error() == "county not found: "+countyName {
			return c.JSON(http.StatusNotFound, map[string]interface{}{
				"success": false,
				"error":   "County not found",
			})
		}
		return c.JSON(http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   "Failed to fetch county: " + err.Error(),
		})
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"success": true,
		"data":    county,
	})
}

// GetCountyBoundaryHandler returns the county boundary in GeoJSON format
func GetCountyBoundaryHandler(c echo.Context) error {
	countyName := c.Param("name")
	if countyName == "" {
		return c.JSON(http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "County name is required",
		})
	}

	boundary, err := services.County.GetCountyBoundaryGeoJSON(countyName)
	if err != nil {
		if err.Error() == "county not found: "+countyName {
			return c.JSON(http.StatusNotFound, map[string]interface{}{
				"success": false,
				"error":   "County not found",
			})
		}
		return c.JSON(http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   "Failed to fetch county boundary: " + err.Error(),
		})
	}

	// Return GeoJSON directly (not wrapped in success/data)
	return c.JSON(http.StatusOK, boundary)
}

// GetCountyStatsHandler returns statistics about all Ohio counties
func GetCountyStatsHandler(c echo.Context) error {
	stats, err := services.County.GetCountyStats()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   "Failed to get county statistics: " + err.Error(),
		})
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"success": true,
		"data":    stats,
	})
}

// GetCountiesInBoundsHandler returns counties within the specified geographic bounds
func GetCountiesInBoundsHandler(c echo.Context) error {
	// Parse bounding box parameters
	minLatStr := c.QueryParam("min_lat")
	minLonStr := c.QueryParam("min_lon")
	maxLatStr := c.QueryParam("max_lat")
	maxLonStr := c.QueryParam("max_lon")

	if minLatStr == "" || minLonStr == "" || maxLatStr == "" || maxLonStr == "" {
		return c.JSON(http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "Bounding box parameters required: min_lat, min_lon, max_lat, max_lon",
		})
	}

	minLat, err := strconv.ParseFloat(minLatStr, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "Invalid min_lat parameter",
		})
	}

	minLon, err := strconv.ParseFloat(minLonStr, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "Invalid min_lon parameter",
		})
	}

	maxLat, err := strconv.ParseFloat(maxLatStr, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "Invalid max_lat parameter",
		})
	}

	maxLon, err := strconv.ParseFloat(maxLonStr, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "Invalid max_lon parameter",
		})
	}

	// Validate bounding box
	if minLat >= maxLat || minLon >= maxLon {
		return c.JSON(http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "Invalid bounding box: min values must be less than max values",
		})
	}

	counties, err := services.County.GetCountiesWithinBounds(minLat, minLon, maxLat, maxLon)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   "Failed to fetch counties in bounds: " + err.Error(),
		})
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"success": true,
		"data":    counties,
		"count":   len(counties),
		"bounds": map[string]float64{
			"min_lat": minLat,
			"min_lon": minLon,
			"max_lat": maxLat,
			"max_lon": maxLon,
		},
	})
}