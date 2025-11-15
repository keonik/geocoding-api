package handlers

import (
	"net/http"
	"strconv"

	"geocoding-api/models"
	"geocoding-api/services"

	"github.com/labstack/echo/v4"
)

// SearchStatesHandler handles GET /api/v1/states - Search for states
func SearchStatesHandler(c echo.Context) error {
	var params models.StateSearchParams
	
	// Parse query parameters
	params.Name = c.QueryParam("name")
	params.Abbr = c.QueryParam("abbr")
	params.Region = c.QueryParam("region")
	params.Division = c.QueryParam("division")

	if lat := c.QueryParam("lat"); lat != "" {
		params.Lat, _ = strconv.ParseFloat(lat, 64)
	}
	if lng := c.QueryParam("lng"); lng != "" {
		params.Lng, _ = strconv.ParseFloat(lng, 64)
	}

	if limit := c.QueryParam("limit"); limit != "" {
		params.Limit, _ = strconv.Atoi(limit)
	}
	if params.Limit <= 0 {
		params.Limit = 50
	}

	if offset := c.QueryParam("offset"); offset != "" {
		params.Offset, _ = strconv.Atoi(offset)
	}

	// If coordinates are provided, use point-in-polygon lookup
	if params.Lat != 0 && params.Lng != 0 {
		state, err := services.State.GetStateByCoordinates(params.Lat, params.Lng)
		if err != nil {
			return c.JSON(http.StatusNotFound, map[string]interface{}{
				"error": "State not found at coordinates",
				"lat":   params.Lat,
				"lng":   params.Lng,
			})
		}

		return c.JSON(http.StatusOK, map[string]interface{}{
			"states": []models.State{*state},
			"total":  1,
			"limit":  1,
			"offset": 0,
		})
	}

	// Otherwise, use text search
	response, err := services.State.SearchStates(params)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]interface{}{
			"error": "Failed to search states",
		})
	}

	return c.JSON(http.StatusOK, response)
}

// GetStateHandler handles GET /api/v1/states/:identifier - Get state details
func GetStateHandler(c echo.Context) error {
	identifier := c.Param("identifier")
	if identifier == "" {
		return c.JSON(http.StatusBadRequest, map[string]interface{}{
			"error": "State identifier is required",
		})
	}

	state, err := services.State.GetStateByIdentifier(identifier)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]interface{}{
			"error": "State not found",
			"identifier": identifier,
		})
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"state": state,
	})
}

// GetStateBoundaryHandler handles GET /api/v1/states/:identifier/boundary - Get state boundary GeoJSON
func GetStateBoundaryHandler(c echo.Context) error {
	identifier := c.Param("identifier")
	if identifier == "" {
		return c.JSON(http.StatusBadRequest, map[string]interface{}{
			"error": "State identifier is required",
		})
	}

	geoJSON, err := services.State.GetStateBoundaryGeoJSON(identifier)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]interface{}{
			"error": "State boundary not found",
			"identifier": identifier,
		})
	}

	return c.JSON(http.StatusOK, geoJSON)
}

// GetStateByLocationHandler handles GET /api/v1/states/lookup - Reverse geocode coordinates to state
func GetStateByLocationHandler(c echo.Context) error {
	latStr := c.QueryParam("lat")
	lngStr := c.QueryParam("lng")

	if latStr == "" || lngStr == "" {
		return c.JSON(http.StatusBadRequest, map[string]interface{}{
			"error": "Both lat and lng parameters are required",
		})
	}

	lat, err := strconv.ParseFloat(latStr, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]interface{}{
			"error": "Invalid latitude value",
		})
	}

	lng, err := strconv.ParseFloat(lngStr, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]interface{}{
			"error": "Invalid longitude value",
		})
	}

	state, err := services.State.GetStateByCoordinates(lat, lng)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]interface{}{
			"error": "No state found at coordinates",
			"lat":   lat,
			"lng":   lng,
		})
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"state": state,
		"coordinates": map[string]float64{
			"lat": lat,
			"lng": lng,
		},
	})
}
