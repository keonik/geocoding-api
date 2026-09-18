package handlers

import (
	"net/http"

	"geocoding-api/services"

	"github.com/labstack/echo/v4"
)

// BatchGeocodeRequest is the POST body.
type BatchGeocodeRequest struct {
	Items []services.BatchItem `json:"items"`
}

// BatchGeocodeHandler resolves many lookups in one request.
//
// The plans have advertised "bulk" as a pro and enterprise feature since long
// before this existed, with nothing behind it.
//
// POST /api/v1/geocode/batch
//
//	{"items": [{"zip_code": "43215"}, {"query": "7057 Barendt"}]}
func BatchGeocodeHandler(c echo.Context) error {
	var req BatchGeocodeRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{
			Success: false,
			Error:   "Body must be JSON: {\"items\": [{\"zip_code\": \"43215\"}]}",
		})
	}

	if len(req.Items) == 0 {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{
			Success: false,
			Error:   "items is empty",
		})
	}
	if len(req.Items) > services.MaxBatchItems {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{
			Success: false,
			Error:   "Batch is larger than the limit",
			Data: map[string]interface{}{
				"items": len(req.Items), "max_items": services.MaxBatchItems,
			},
		})
	}

	// The quota check in APIKeyAuth ran before this handler, when the batch
	// size was still unknown -- it only established that the caller had at
	// least one call left. A caller one call from their limit could otherwise
	// spend a hundred here, so the remaining allowance is checked against the
	// actual size before doing the work.
	if status, ok := c.Get("rate_limit_status").(*services.RateLimitStatus); ok && !status.Unlimited() {
		if remaining, limited := remainingAllowance(status); limited && len(req.Items) > remaining {
			return c.JSON(http.StatusTooManyRequests, GeocodeResponse{
				Success: false,
				Error:   "Batch is larger than your remaining allowance",
				Data: map[string]interface{}{
					"items":     len(req.Items),
					"remaining": remaining,
					"message":   "Split the batch or wait for the limit to reset",
				},
			})
		}
	}

	// The same check against the key's own cap, which can be tighter than the
	// plan. Without it a key capped at 50 submits a batch of 100.
	if ks, ok := c.Get(services.KeyLimitStatusKey).(*services.KeyLimitStatus); ok {
		if remaining, capped := ks.Remaining(); capped && len(req.Items) > remaining {
			return c.JSON(http.StatusTooManyRequests, GeocodeResponse{
				Success: false,
				Error:   "Batch is larger than this API key's remaining allowance",
				Data: map[string]interface{}{
					"items":     len(req.Items),
					"remaining": remaining,
					"scope":     "key",
					"message":   "Split the batch, or raise this key's own cap",
				},
			})
		}
	}

	result, err := services.BatchGeocode(services.GetDB(), req.Items)
	if err != nil {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{
			Success: false,
			Error:   err.Error(),
		})
	}

	// Bill and rate-limit this as the number of lookups it performed. Without
	// this a batch of 100 counts as a single call, which both under-bills and
	// lets a caller walk past their own monthly limit by wrapping every lookup
	// in a batch.
	c.Set(services.BillableUnitsKey, result.BillableUnits)

	return c.JSON(http.StatusOK, GeocodeResponse{Success: true, Data: result})
}

// remainingAllowance is how many more lookups the caller may perform before the
// tighter of their two limits stops them.
func remainingAllowance(status *services.RateLimitStatus) (int, bool) {
	remaining := -1

	if status.DailyLimit > 0 {
		remaining = status.DailyLimit - status.DailyUsage
	}
	if status.MonthlyLimit > 0 {
		monthly := status.MonthlyLimit - status.MonthlyUsage
		if remaining < 0 || monthly < remaining {
			remaining = monthly
		}
	}

	if remaining < 0 {
		return 0, false
	}
	return remaining, true
}
