package handlers

import (
	"net/http"

	"geocoding-api/services"

	"github.com/labstack/echo/v4"
)

// ValidateAddressRequest is the POST body.
type ValidateAddressRequest struct {
	Address string `json:"address"`
}

// ValidateAddressHandler answers whether an address is real and what it really
// is.
//
// Distinct from search, which answers "what matches this". A checkout form or a
// CRM import does not want a ranked list -- it wants to know whether to store
// what the user typed, store something corrected, or ask again.
//
// POST /api/v1/address/validate
//
//	{"address": "7057 barendt rd, toledo oh"}
func ValidateAddressHandler(c echo.Context) error {
	var req ValidateAddressRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{
			Success: false,
			Error:   "Body must be JSON: {\"address\": \"...\"}",
		})
	}

	result, err := services.ValidateAddress(services.GetDB(), req.Address)
	if err != nil {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{
			Success: false,
			Error:   err.Error(),
		})
	}

	// A well-formed address that does not exist is a successful answer to the
	// question asked, not a failure. The caller reads `verified`.
	return c.JSON(http.StatusOK, GeocodeResponse{Success: true, Data: result})
}
