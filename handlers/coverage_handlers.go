package handlers

import (
	"net/http"
	"strings"

	"geocoding-api/services"

	"github.com/labstack/echo/v4"
)

// GetCoverageHandler reports which states and counties have address data.
//
// Street-level data is currently Ohio only. A caller querying anywhere else
// gets an empty result set, which looks exactly like a malformed query or a
// broken service -- so they open a ticket. This makes that self-service.
//
// GET /api/v1/coverage            summary by state
// GET /api/v1/coverage?state=OH   per-county breakdown for one state
func GetCoverageHandler(c echo.Context) error {
	db := services.GetDB()

	if state := strings.TrimSpace(c.QueryParam("state")); state != "" {
		counties, err := services.GetStateCoverage(db, state)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, GeocodeResponse{
				Success: false,
				Error:   "Failed to read coverage",
			})
		}

		return c.JSON(http.StatusOK, GeocodeResponse{
			Success: true,
			Data: map[string]interface{}{
				"state":    strings.ToUpper(state),
				"counties": counties,
				"count":    len(counties),
			},
		})
	}

	snapshot, err := services.GetCoverage(db)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, GeocodeResponse{
			Success: false,
			Error:   "Failed to read coverage",
		})
	}

	return c.JSON(http.StatusOK, GeocodeResponse{
		Success: true,
		Data:    snapshot,
	})
}
