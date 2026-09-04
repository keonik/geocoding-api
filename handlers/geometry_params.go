package handlers

import (
	"strconv"

	"geocoding-api/services"

	"github.com/labstack/echo/v4"
)

// Defaults and caps for the boundary endpoints' cost knobs.
const (
	// The default lives in services.DefaultBoundaryTolerance because migration
	// 19 materialises exactly that tolerance; requesting it is a column read,
	// requesting anything else simplifies on the fly and is ~80x slower.
	maxGeometryTolerance = 0.05 // ~5.5 km; past this a state stops being recognisable

	// 6 decimal places is ~11 cm. The source data carries ~15, which is noise.
	defaultGeometryPrecision = 6
	minGeometryPrecision     = 1
	maxGeometryPrecision     = 9
)

// geometryParams reads the shared display-cost knobs for the boundary
// endpoints. Callers that genuinely need full resolution pass tolerance=0.
// Out-of-range or unparseable values fall back to the defaults rather than
// erroring, so a bad query string degrades to a cheap response instead of a
// 400.
func geometryParams(c echo.Context) (float64, int) {
	tolerance := services.DefaultBoundaryTolerance
	if v := c.QueryParam("tolerance"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 && f <= maxGeometryTolerance {
			tolerance = f
		}
	}

	precision := defaultGeometryPrecision
	if v := c.QueryParam("precision"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= minGeometryPrecision && n <= maxGeometryPrecision {
			precision = n
		}
	}

	return tolerance, precision
}
