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
	lat, lng, ok, err := parseLatLng(c, "/api/v1/reverse?lat=39.9612&lng=-83.0007")
	if !ok {
		return err
	}

	// Enrichment is opt-in here, unlike on /enrich: it is a second query set,
	// and a caller who asked what is at a point did not ask for its districts.
	var layers []services.BoundaryLayer
	if raw := c.QueryParam("fields"); raw != "" {
		if layers, err = services.ParseEnrichmentFields(raw); err != nil {
			return c.JSON(http.StatusBadRequest, GeocodeResponse{Success: false, Error: err.Error()})
		}
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

	if layers != nil {
		if result.Enrichment, err = services.Enrich(services.GetDB(), lat, lng, layers); err != nil {
			return c.JSON(http.StatusInternalServerError, GeocodeResponse{Success: false, Error: err.Error()})
		}
	}

	return c.JSON(http.StatusOK, GeocodeResponse{Success: true, Data: result})
}
