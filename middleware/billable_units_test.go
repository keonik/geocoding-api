package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"geocoding-api/services"

	"github.com/labstack/echo/v4"
)

// Every endpoint but batch performs one lookup, so an absent declaration must
// mean one rather than zero -- a zero would bill nothing at all.
func TestBillableUnitsDefaultsToOne(t *testing.T) {
	e := echo.New()
	c := e.NewContext(httptest.NewRequest(http.MethodGet, "/", nil), httptest.NewRecorder())

	if got := BillableUnits(c); got != 1 {
		t.Errorf("with nothing declared, units = %d, want 1", got)
	}
}

// A batch declares its item count so it is billed and rate-limited as that many
// calls. Without this a batch of 100 counts once: the caller is under-billed
// 100x and walks past their own monthly limit by wrapping every lookup in a
// batch.
func TestBillableUnitsReadsWhatTheHandlerDeclared(t *testing.T) {
	e := echo.New()
	c := e.NewContext(httptest.NewRequest(http.MethodPost, "/", nil), httptest.NewRecorder())

	c.Set(services.BillableUnitsKey, 100)
	if got := BillableUnits(c); got != 100 {
		t.Errorf("units = %d, want 100", got)
	}
}

// Nonsense must not reduce what a caller is charged.
func TestBillableUnitsIgnoresValuesBelowOne(t *testing.T) {
	e := echo.New()
	for _, v := range []interface{}{0, -5, "many", nil} {
		c := e.NewContext(httptest.NewRequest(http.MethodPost, "/", nil), httptest.NewRecorder())
		c.Set(services.BillableUnitsKey, v)
		if got := BillableUnits(c); got != 1 {
			t.Errorf("with %v declared, units = %d, want 1", v, got)
		}
	}
}
