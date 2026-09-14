package handlers

import (
	"net/http"

	"geocoding-api/services"

	"github.com/labstack/echo/v4"
)

// GetDataQualityHandler reports the silent correctness problems in the address
// data.
//
// Everything it surfaces is wrong in a way that raises no error: a search still
// returns rows, an import still reports success, and nothing in the logs says
// otherwise. Production held eleven distinct region codes -- including ON
// (Ontario), BE, IH, PJ and a bare 0 -- plus 985,634 addresses with no state at
// all, and none of it was visible anywhere. It surfaced only because an
// unrelated endpoint happened to group by region.
//
// Admin-only: it is an operational view of data health, and the scan is
// expensive enough to be worth restricting.
func GetDataQualityHandler(c echo.Context) error {
	report, err := services.GetDataQuality(services.GetDB())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, GeocodeResponse{
			Success: false,
			Error:   "Failed to scan address data",
		})
	}

	return c.JSON(http.StatusOK, GeocodeResponse{
		Success: true,
		Data:    report,
	})
}
