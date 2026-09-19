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
// GET /api/v1/reverse?lat=39.9612&lng=-83.0007[&radius=2000][&fields=census,cd]
func ReverseGeocodeHandler(c echo.Context) error {
	lat, lng, problem := parseLatLng(c)
	if problem != "" {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{
			Success: false,
			Error:   problem,
			Data:    map[string]interface{}{"example": "/api/v1/reverse?lat=39.9612&lng=-83.0007"},
		})
	}

	// Enrichment is opt-in here, unlike on /enrich: it is a second query set,
	// and a caller who asked what is at a point did not ask for its districts.
	var enrich *services.EnrichmentRequest
	var err error
	if raw := c.QueryParam("fields"); raw != "" {
		req, perr := services.ParseEnrichmentFields(raw)
		if perr != nil {
			return c.JSON(http.StatusBadRequest, GeocodeResponse{Success: false, Error: perr.Error()})
		}
		enrich = &req
	}

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

	// The range was checked above, so what fails here is the server.
	result, err := services.ReverseGeocode(services.GetDB(), lat, lng, radius)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, GeocodeResponse{
			Success: false,
			Error:   err.Error(),
		})
	}

	if enrich != nil {
		if result.Enrichment, err = services.Enrich(services.GetDB(), lat, lng, *enrich); err != nil {
			return c.JSON(http.StatusInternalServerError, GeocodeResponse{Success: false, Error: err.Error()})
		}
	}

	return c.JSON(http.StatusOK, GeocodeResponse{Success: true, Data: result})
}
