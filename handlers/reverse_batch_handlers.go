package handlers

import (
	"net/http"

	"geocoding-api/services"

	"github.com/labstack/echo/v4"
)

// ReverseBatchRequest is the POST body.
type ReverseBatchRequest struct {
	Items []services.ReverseBatchItem `json:"items"`
	// Radius is how far to look for an address, in metres, applied to every
	// item. Per request rather than per item: a caller resolving a route's
	// worth of coordinates wants one rule for the lot, and a mixed batch makes
	// the search_radius_meters in the response meaningless.
	Radius float64 `json:"radius,omitempty"`
}

// ReverseBatchHandler resolves many coordinates in one request.
//
// /reverse answered one point per request, so a caller with a day of GPS
// traces had to make a request per fix -- and pay the round trip for each. The
// forward direction has had a batch endpoint for a while; this is the one that
// was missing.
//
// POST /api/v1/reverse/batch
//
//	{"items": [{"id": "1", "lat": 39.9612, "lng": -83.0007}], "radius": 2000}
func ReverseBatchHandler(c echo.Context) error {
	var req ReverseBatchRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{
			Success: false,
			Error:   "Body must be JSON: {\"items\": [{\"lat\": 39.9612, \"lng\": -83.0007}]}",
		})
	}
	if refused, err := refuseBatch(c, len(req.Items)); refused {
		return err
	}

	// refuseBatch has already rejected an empty or oversized batch, and each
	// item's coordinates are checked per item, so what fails here is the
	// server -- as on /reverse, which answers 500 for the same faults. This
	// returned 400 at first, which told a caller their request was wrong when
	// a table was missing.
	result, err := services.ReverseGeocodeBatch(services.GetDB(), req.Items, req.Radius)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, GeocodeResponse{Success: false, Error: err.Error()})
	}

	// Billed and rate-limited as the lookups it performed, like the forward
	// batch: a hundred coordinates is a hundred lookups however few requests
	// carry them.
	c.Set(services.BillableUnitsKey, result.BillableUnits)

	return c.JSON(http.StatusOK, GeocodeResponse{Success: true, Data: result})
}
