package handlers

import (
	"net/http"
	"strconv"

	"geocoding-api/services"

	"github.com/labstack/echo/v4"
)

// ReverseGeocodeHandler resolves a coordinate to what is there.
//
// The counterpart to forward geocoding. Until now the API could only answer
// point-to-state, via /states/lookup.
//
// GET /api/v1/reverse?lat=39.9612&lng=-83.0007[&radius=2000]
func ReverseGeocodeHandler(c echo.Context) error {
	latRaw := c.QueryParam("lat")
	lngRaw := c.QueryParam("lng")
	if latRaw == "" || lngRaw == "" {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{
			Success: false,
			Error:   "Both lat and lng are required",
			Data:    map[string]interface{}{"example": "/api/v1/reverse?lat=39.9612&lng=-83.0007"},
		})
	}

	lat, err := strconv.ParseFloat(latRaw, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{
			Success: false,
			Error:   "lat is not a number",
		})
	}
	lng, err := strconv.ParseFloat(lngRaw, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{
			Success: false,
			Error:   "lng is not a number",
		})
	}

	// Rejected rather than clamped: a caller who passes them the wrong way
	// round gets told, instead of a confident answer about somewhere else.
	var radius float64
	if raw := c.QueryParam("radius"); raw != "" {
		radius, err = strconv.ParseFloat(raw, 64)
		if err != nil {
			return c.JSON(http.StatusBadRequest, GeocodeResponse{
				Success: false,
				Error:   "radius is not a number (metres)",
			})
		}
	}

	result, err := services.ReverseGeocode(services.GetDB(), lat, lng, radius)
	if err != nil {
		// The service validates the coordinate range, and that is the caller's
		// mistake rather than a server fault.
		return c.JSON(http.StatusBadRequest, GeocodeResponse{
			Success: false,
			Error:   err.Error(),
		})
	}

	return c.JSON(http.StatusOK, GeocodeResponse{Success: true, Data: result})
}
