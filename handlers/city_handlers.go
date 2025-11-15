package handlers

import (
	"geocoding-api/models"
	"geocoding-api/services"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
)

// SearchCitiesHandler handles city search requests
func SearchCitiesHandler(c echo.Context) error {
	var params models.CitySearchParams
	
	// Parse query parameters
	params.Query = c.QueryParam("query")
	params.City = c.QueryParam("city")
	params.State = c.QueryParam("state")
	params.County = c.QueryParam("county")
	
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
	if minPop := c.QueryParam("min_population"); minPop != "" {
		if val, err := strconv.Atoi(minPop); err == nil {
			params.MinPop = val
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

	// Search cities
	cities, total, err := services.City.SearchCities(params)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, models.CitySearchResponse{
			Success: false,
			Error:   "Failed to search cities: " + err.Error(),
		})
	}

	// Prepare filters for response
	filters := make(map[string]interface{})
	if params.City != "" {
		filters["city"] = params.City
	}
	if params.State != "" {
		filters["state"] = params.State
	}
	if params.County != "" {
		filters["county"] = params.County
	}
	if params.MinPop > 0 {
		filters["min_population"] = params.MinPop
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

	return c.JSON(http.StatusOK, models.CitySearchResponse{
		Success: true,
		Data:    cities,
		Count:   len(cities),
		Total:   total,
		Query:   params.Query,
		Filters: filters,
	})
}

// GetCityHandler retrieves a specific city by ID
func GetCityHandler(c echo.Context) error {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, models.CitySearchResponse{
			Success: false,
			Error:   "Invalid city ID",
		})
	}

	city, err := services.City.GetCityByID(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, models.CitySearchResponse{
			Success: false,
			Error:   "City not found",
		})
	}

	return c.JSON(http.StatusOK, models.CitySearchResponse{
		Success: true,
		Data:    []models.City{*city},
		Count:   1,
		Total:   1,
	})
}

// GetCityZIPCodesHandler returns ZIP codes for a city
func GetCityZIPCodesHandler(c echo.Context) error {
	city := c.QueryParam("city")
	state := c.QueryParam("state")

	if city == "" || state == "" {
		return c.JSON(http.StatusBadRequest, models.CitySearchResponse{
			Success: false,
			Error:   "Both 'city' and 'state' parameters are required",
		})
	}

	zips, err := services.City.GetZIPCodesForCity(city, state)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, models.CitySearchResponse{
			Success: false,
			Error:   "Failed to get ZIP codes: " + err.Error(),
		})
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"city":  city,
			"state": state,
			"zips":  zips,
			"count": len(zips),
		},
	})
}
