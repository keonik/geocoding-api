package handlers

import (
	"fmt"
	"geocoding-api/models"
	"geocoding-api/services"
	"net/http"
	"strconv"

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
