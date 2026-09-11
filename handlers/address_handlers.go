package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"geocoding-api/models"
	"geocoding-api/services"

	"github.com/labstack/echo/v4"
)

// SearchOhioAddressesHandler handles address search requests
func SearchOhioAddressesHandler(c echo.Context) error {
	var params models.AddressSearchParams

	// Manually parse query parameters (Echo's Bind doesn't always work for query params)
	params.Query = c.QueryParam("query")
	params.County = c.QueryParam("county")
	params.City = c.QueryParam("city")
	params.Postcode = c.QueryParam("postcode")
	params.Street = c.QueryParam("street")

	// Parse numeric parameters
	if lat := c.QueryParam("lat"); lat != "" {
		if val, err := strconv.ParseFloat(lat, 64); err == nil {
			params.Lat = val
		}
	}
	if lng := c.QueryParam("lng"); lng != "" {
		if val, err := strconv.ParseFloat(lng, 64); err == nil {
			params.Lng = val
		}
	}
	if radius := c.QueryParam("radius"); radius != "" {
		if val, err := strconv.ParseFloat(radius, 64); err == nil {
			params.Radius = val
		}
	}

	// Territory filters. Unlike the numeric parameters above, a malformed
	// value here is reported rather than ignored: silently dropping a bbox
	// widens the search to the whole state, and a caller asking for one
	// neighbourhood would get 50 arbitrary rows back and no indication why.
	if raw := c.QueryParam("bbox"); raw != "" {
		box, err := models.ParseBBox(raw)
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]interface{}{
				"success": false,
				"error":   err.Error(),
				"example": "bbox=-83.1,39.9,-82.9,40.1",
			})
		}
		params.BBox = box
	}

	if raw := c.QueryParam("polygon"); raw != "" {
		if err := validateGeoJSONPolygon(raw); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]interface{}{
				"success": false,
				"error":   err.Error(),
				"example": `polygon={"type":"Polygon","coordinates":[[[-83.1,39.9],[-82.9,39.9],[-82.9,40.1],[-83.1,40.1],[-83.1,39.9]]]}`,
			})
		}
		params.Polygon = raw
	}
	if limit := c.QueryParam("limit"); limit != "" {
		if val, err := strconv.Atoi(limit); err == nil {
			params.Limit = val
		}
	}
	if offset := c.QueryParam("offset"); offset != "" {
		if val, err := strconv.Atoi(offset); err == nil {
			params.Offset = val
		}
	}

	// Search addresses
	addresses, total, err := services.Address.SearchAddresses(params)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, models.AddressSearchResponse{
			Success: false,
			Error:   "Failed to search addresses: " + err.Error(),
		})
	}

	// Prepare filters for response
	filters := make(map[string]any)
	if params.County != "" {
		filters["county"] = params.County
	}
	if params.City != "" {
		filters["city"] = params.City
	}
	if params.Postcode != "" {
		filters["postcode"] = params.Postcode
	}
	if params.Street != "" {
		filters["street"] = params.Street
	}
	if params.Lat != 0 && params.Lng != 0 {
		filters["location"] = map[string]float64{
			"lat": params.Lat,
			"lng": params.Lng,
		}
		if params.Radius > 0 {
			filters["radius_km"] = params.Radius
		}
	}
	if params.BBox != nil {
		filters["bbox"] = params.BBox
	}
	if params.Polygon != "" {
		// Echoed as a flag, not as the shape. A territory polygon can run to
		// hundreds of vertices, and repeating it would dwarf the results the
		// caller asked for.
		filters["polygon"] = true
	}

	return c.JSON(http.StatusOK, models.AddressSearchResponse{
		Success: true,
		Data:    addresses,
		Count:   len(addresses),
		Total:   total,
		Query:   params.Query,
		Filters: filters,
	})
}

// GetOhioAddressHandler retrieves a specific address by ID
func GetOhioAddressHandler(c echo.Context) error {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, models.AddressSearchResponse{
			Success: false,
			Error:   "Invalid address ID",
		})
	}

	address, err := services.Address.GetAddressByID(id)
	if err != nil {
		if err.Error() == "address not found" {
			return c.JSON(http.StatusNotFound, models.AddressSearchResponse{
				Success: false,
				Error:   "Address not found",
			})
		}
		return c.JSON(http.StatusInternalServerError, models.AddressSearchResponse{
			Success: false,
			Error:   "Failed to get address: " + err.Error(),
		})
	}

	return c.JSON(http.StatusOK, models.AddressSearchResponse{
		Success: true,
		Data:    []models.OhioAddress{*address},
		Count:   1,
	})
}

// GetOhioCountyStatsHandler returns statistics about Ohio counties
func GetOhioCountyStatsHandler(c echo.Context) error {
	stats, err := services.Address.GetCountyStats()
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

// FullTextSearchAddressesHandler handles full-text address search requests
func FullTextSearchAddressesHandler(c echo.Context) error {
	query := c.QueryParam("q")
	if query == "" {
		return c.JSON(http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "Query parameter 'q' is required",
		})
	}

	// Parse limit parameter
	limit := 50 // Default
	if limitStr := c.QueryParam("limit"); limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 && parsedLimit <= 500 {
			limit = parsedLimit
		}
	}

	// Perform full-text search
	result, err := services.Address.FullTextSearchAddresses(query, limit)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   "Failed to search addresses: " + err.Error(),
		})
	}

	response := map[string]interface{}{
		"success":       true,
		"data":          result.Addresses,
		"count":         len(result.Addresses),
		"exact_count":   result.ExactCount,
		"query":         query,
		"search_method": result.SearchMethod,
	}

	if result.ParsedQuery != nil {
		response["parsed_as"] = result.ParsedQuery
	}

	// Add fallback information if street-level matches were included
	if result.FallbackCount > 0 {
		response["fallback_count"] = result.FallbackCount
		response["fallback_query"] = result.FallbackQuery
		response["message"] = fmt.Sprintf("Found %d exact matches and %d additional addresses on the same street.",
			result.ExactCount, result.FallbackCount)
	}

	return c.JSON(http.StatusOK, response)
}

// validateGeoJSONPolygon checks the shape before it reaches PostGIS.
//
// ST_GeomFromGeoJSON raises on malformed input, which would surface as a 500
// on a request that is simply wrong -- a caller's typo should not look like a
// server fault. Checking here also keeps the error specific: "not a Polygon"
// is actionable, "failed to search addresses" is not.
func validateGeoJSONPolygon(raw string) error {
	var shape struct {
		Type        string        `json:"type"`
		Coordinates [][][]float64 `json:"coordinates"`
	}
	if err := json.Unmarshal([]byte(raw), &shape); err != nil {
		return fmt.Errorf("polygon is not valid GeoJSON: %v", err)
	}

	switch shape.Type {
	case "Polygon":
	case "":
		return fmt.Errorf(`polygon is missing its "type" field; expected {"type":"Polygon","coordinates":[...]}`)
	default:
		return fmt.Errorf("polygon type is %q; only Polygon is supported", shape.Type)
	}

	if len(shape.Coordinates) == 0 || len(shape.Coordinates[0]) < 4 {
		return fmt.Errorf("polygon needs a ring of at least 4 positions (the last repeating the first)")
	}

	ring := shape.Coordinates[0]
	first, last := ring[0], ring[len(ring)-1]
	if len(first) < 2 || len(last) < 2 {
		return fmt.Errorf("each polygon position needs at least a longitude and a latitude")
	}
	// GeoJSON requires a closed ring. PostGIS rejects an open one, and the
	// message it produces does not say which ring or why.
	if first[0] != last[0] || first[1] != last[1] {
		return fmt.Errorf("polygon ring is not closed: the last position must repeat the first")
	}

	for i, pos := range ring {
		if len(pos) < 2 {
			return fmt.Errorf("position %d is missing a coordinate", i)
		}
		if pos[0] < -180 || pos[0] > 180 {
			return fmt.Errorf("position %d has longitude %g outside -180..180 (GeoJSON is longitude first)", i, pos[0])
		}
		if pos[1] < -90 || pos[1] > 90 {
			return fmt.Errorf("position %d has latitude %g outside -90..90 (GeoJSON is longitude first)", i, pos[1])
		}
	}

	return nil
}
