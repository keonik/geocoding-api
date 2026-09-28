package handlers

import (
	"math"
	"net/http"
	"strconv"
	"time"

	"geocoding-api/models"
	"geocoding-api/services"

	"github.com/labstack/echo/v4"
)

// refuseBatch checks a batch's size against everything that governs it.
//
// refused says whether a refusal has been written, and the handler must then
// return err without doing the work. The two are separate because c.JSON
// returns nil when it writes successfully: a helper that returned only that
// error would report "no error" for a refusal it had just written, and the
// caller would carry on and do the work anyway. That is not hypothetical --
// it is what the first version of this did, and what TestBatchIsChargedByItsItems
// caught.
//
// Shared by every batch endpoint. The checks are not optional decoration: each
// one closes a hole that opens because APIKeyAuth ran before the body was
// read, when the batch's size was still unknown.
func refuseBatch(c echo.Context, items int) (refused bool, err error) {
	if items == 0 {
		return true, c.JSON(http.StatusBadRequest, GeocodeResponse{
			Success: false,
			Error:   "items is empty",
		})
	}
	if items > services.MaxBatchItems {
		return true, c.JSON(http.StatusBadRequest, GeocodeResponse{
			Success: false,
			Error:   "Batch is larger than the limit",
			Data: map[string]interface{}{
				"items": items, "max_items": services.MaxBatchItems,
			},
		})
	}

	// The quota check in APIKeyAuth ran before this handler, when the batch
	// size was still unknown -- it only established that the caller had at
	// least one call left. A caller one call from their limit could otherwise
	// spend a hundred here, so the remaining allowance is checked against the
	// actual size before doing the work.
	if status, ok := c.Get("rate_limit_status").(*services.RateLimitStatus); ok && !status.Unlimited() {
		if remaining, limited := remainingAllowance(status); limited && items > remaining {
			return true, c.JSON(http.StatusTooManyRequests, GeocodeResponse{
				Success: false,
				Error:   "Batch is larger than your remaining allowance",
				Data: map[string]interface{}{
					"items":     items,
					"remaining": remaining,
					"message":   "Split the batch or wait for the limit to reset",
				},
			})
		}
	}

	// The burst bucket counts lookups, and the middleware could only charge
	// one: the batch's size was still in the unread body. The rest is
	// charged here, before the work. Without it a batch is a hundred lookups
	// for the price of one token, and a caller at the allowed request rate
	// drives a hundred times the allowed lookup rate -- on the heaviest path
	// there is.
	if perSecond, ok := c.Get(services.BurstLimitKey).(int); ok && perSecond > 0 && items > 1 {
		if key, ok := c.Get("api_key").(*models.APIKey); ok {
			if allowed, wait := services.KeyBursts.AllowN(key.ID, perSecond, items-1, time.Now()); !allowed {
				retryAfter := int(math.Ceil(wait.Seconds()))
				if retryAfter < 1 {
					retryAfter = 1
				}
				c.Response().Header().Set("Retry-After", strconv.Itoa(retryAfter))
				c.Response().Header().Set("X-RateLimit-Scope", services.ScopeBurst)
				c.Response().Header().Set("X-RateLimit-Limit-Second", strconv.Itoa(perSecond))
				return true, c.JSON(http.StatusTooManyRequests, GeocodeResponse{
					Success: false,
					Error:   "This batch is more lookups per second than the key's rate allows",
					Data: map[string]interface{}{
						"items":       items,
						"limit":       perSecond,
						"limit_scope": services.ScopeBurst,
						"retry_after": retryAfter,
						"message":     "Wait the stated seconds, or send a smaller batch",
					},
				})
			}
		}
	}

	// The same check against the key's own cap, which can be tighter than the
	// plan. Without it a key capped at 50 submits a batch of 100.
	if ks, ok := c.Get(services.KeyLimitStatusKey).(*services.KeyLimitStatus); ok {
		if remaining, capped := ks.Remaining(); capped && items > remaining {
			return true, c.JSON(http.StatusTooManyRequests, GeocodeResponse{
				Success: false,
				Error:   "Batch is larger than this API key's remaining allowance",
				Data: map[string]interface{}{
					"items":     items,
					"remaining": remaining,
					"scope":     "key",
					"message":   "Split the batch, or raise this key's own cap",
				},
			})
		}
	}

	return false, nil
}
