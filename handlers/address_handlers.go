package handlers

import (
	"geocoding-api/models"
	"geocoding-api/services"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
)

// SearchOhioAddressesHandler handles address search requests
func SearchOhioAddressesHandler(c echo.Context) error {
	var params models.AddressSearchParams
	
	// Bind query parameters
	if err := c.Bind(&params); err != nil {
		return c.JSON(http.StatusBadRequest, models.AddressSearchResponse{
			Success: false,
			Error:   "Invalid search parameters",
		})
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