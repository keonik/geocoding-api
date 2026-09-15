package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"geocoding-api/services"

	"github.com/labstack/echo/v4"
)

// mvtContentType is what every vector tile client expects.
const mvtContentType = "application/vnd.mapbox-vector-tile"

// GetTileHandler serves county and state outlines as Mapbox Vector Tiles.
//
// The geometry has been in the database all along, and migration 19 already
// precomputed the simplified version -- which was the expensive half of tile
// serving. ST_AsMVT does the encoding in Postgres, so nothing is serialised
// through Go.
//
// GET /api/v1/tiles/{layer}/{z}/{x}/{y}.mvt
//
// Layers are separate rather than combined because the scope a caller needs
// differs: counties and states are distinct permissions on every other
// endpoint, and one tile carrying both would have to demand both.
func GetTileHandler(c echo.Context) error {
	layerName := c.Param("layer")
	layer, ok := services.TileLayers[layerName]
	if !ok {
		known := make([]string, 0, len(services.TileLayers))
		for name := range services.TileLayers {
			known = append(known, name)
		}
		return c.JSON(http.StatusNotFound, GeocodeResponse{
			Success: false,
			Error:   "Unknown tile layer",
			Data:    map[string]interface{}{"layer": layerName, "available": known},
		})
	}

	z, err := strconv.Atoi(c.Param("z"))
	if err != nil {
		return tileBadRequest(c, "zoom is not a number")
	}
	x, err := strconv.Atoi(c.Param("x"))
	if err != nil {
		return tileBadRequest(c, "x is not a number")
	}
	// The .mvt suffix is conventional in tile URLs and carries no information,
	// so it is accepted and discarded rather than required.
	y, err := strconv.Atoi(strings.TrimSuffix(c.Param("y"), ".mvt"))
	if err != nil {
		return tileBadRequest(c, "y is not a number")
	}

	if err := services.ValidTileCoordinates(z, x, y); err != nil {
		return tileBadRequest(c, err.Error())
	}

	tile, err := services.RenderTile(services.GetDB(), layer, z, x, y)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, GeocodeResponse{
			Success: false,
			Error:   "Failed to render tile",
		})
	}

	// Most tiles in a pyramid are empty. 204 says so in a way every client
	// understands, and saves them parsing an encoding with nothing in it.
	if tile == nil {
		return c.NoContent(http.StatusNoContent)
	}

	return c.Blob(http.StatusOK, mvtContentType, tile)
}

func tileBadRequest(c echo.Context, message string) error {
	return c.JSON(http.StatusBadRequest, GeocodeResponse{
		Success: false,
		Error:   message,
		Data:    map[string]interface{}{"example": "/api/v1/tiles/counties/8/70/97.mvt"},
	})
}
