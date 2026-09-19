package handlers

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"geocoding-api/services"

	"github.com/labstack/echo/v4"
)

// parseLatLng reads the lat and lng query parameters, or writes the 400 that
// explains what is wrong with them. ok is false when the response is written.
func parseLatLng(c echo.Context, example string) (lat, lng float64, ok bool, err error) {
	latRaw := c.QueryParam("lat")
	lngRaw := c.QueryParam("lng")
	if latRaw == "" || lngRaw == "" {
		return 0, 0, false, c.JSON(http.StatusBadRequest, GeocodeResponse{
			Success: false,
			Error:   "Both lat and lng are required",
			Data:    map[string]interface{}{"example": example},
		})
	}
	lat, perr := strconv.ParseFloat(latRaw, 64)
	if perr != nil {
		return 0, 0, false, c.JSON(http.StatusBadRequest, GeocodeResponse{Success: false, Error: "lat is not a number"})
	}
	lng, perr = strconv.ParseFloat(lngRaw, 64)
	if perr != nil {
		return 0, 0, false, c.JSON(http.StatusBadRequest, GeocodeResponse{Success: false, Error: "lng is not a number"})
	}
	return lat, lng, true, nil
}

// EnrichResponse is a point and the districts it falls in.
type EnrichResponse struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
	*services.Enrichment
}

// EnrichHandler reports the Census geographies and districts containing a
// point: tract, block group, block, congressional and state legislative
// districts, school districts, and incorporated place.
//
// GET /api/v1/enrich?lat=39.9612&lng=-83.0007[&fields=census,cd]
func EnrichHandler(c echo.Context) error {
	lat, lng, ok, err := parseLatLng(c, "/api/v1/enrich?lat=39.9612&lng=-83.0007&fields=census,cd")
	if !ok {
		return err
	}
	layers, err := services.ParseEnrichmentFields(c.QueryParam("fields"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{Success: false, Error: err.Error()})
	}

	result, err := services.Enrich(services.GetDB(), lat, lng, layers)
	if err != nil {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{Success: false, Error: err.Error()})
	}
	return c.JSON(http.StatusOK, GeocodeResponse{
		Success: true,
		Data:    EnrichResponse{Lat: lat, Lng: lng, Enrichment: result},
	})
}

// LoadBoundariesRequest names what to load. An empty layers list means every
// layer except blocks, which are an order of magnitude larger than the rest
// and worth loading only where block-level answers are needed.
type LoadBoundariesRequest struct {
	State  string   `json:"state"`
	Layers []string `json:"layers"`
}

// boundaryLoadTimeout bounds one layer. Ohio's blocks, the largest file, take
// a few minutes; this is generous without letting a stalled download hold its
// claim forever.
const boundaryLoadTimeout = 30 * time.Minute

// LoadBoundariesHandler starts loading Census boundary layers for a state.
//
// The load runs after the response: a state's block groups alone are a 19 MB
// download and ten thousand polygons, longer than a proxy will hold a request
// open. Progress is read from GET /admin/boundaries.
//
// POST /api/v1/admin/boundaries/load {"state": "OH", "layers": ["tract", "cd"]}
func LoadBoundariesHandler(c echo.Context) error {
	var req LoadBoundariesRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{Success: false, Error: "Malformed body"})
	}
	db := services.GetDB()
	fips, err := services.ResolveStateFIPS(db, req.State)
	if err != nil {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{Success: false, Error: err.Error()})
	}

	var layers []services.BoundaryLayer
	if len(req.Layers) == 0 {
		for _, l := range services.BoundaryLayers {
			if l.Name != "block" {
				layers = append(layers, l)
			}
		}
	}
	for _, name := range req.Layers {
		l, ok := services.BoundaryLayerByName(name)
		if !ok {
			return c.JSON(http.StatusBadRequest, GeocodeResponse{Success: false, Error: "unknown layer " + strconv.Quote(name)})
		}
		layers = append(layers, l)
	}

	var started []services.BoundaryLayer
	busy := []string{}
	for _, l := range layers {
		err := services.BeginBoundaryLoad(db, l, fips)
		if errors.Is(err, services.ErrBoundaryLoadInProgress) {
			busy = append(busy, l.Name)
			continue
		}
		if err != nil {
			return c.JSON(http.StatusInternalServerError, GeocodeResponse{Success: false, Error: err.Error()})
		}
		started = append(started, l)
	}

	go func() {
		for _, l := range started {
			ctx, cancel := context.WithTimeout(context.Background(), boundaryLoadTimeout)
			n, err := services.LoadBoundaryLayer(ctx, db, l, fips)
			cancel()
			if err != nil {
				log.Printf("boundary load %s/%s failed: %v", l.Name, fips, err)
				continue
			}
			log.Printf("boundary load %s/%s: %d features", l.Name, fips, n)
		}
	}()

	names := make([]string, len(started))
	for i, l := range started {
		names[i] = l.Name
	}
	return c.JSON(http.StatusAccepted, GeocodeResponse{
		Success: true,
		Data: map[string]interface{}{
			"state_fips":          fips,
			"started":             names,
			"already_in_progress": busy,
			"status":              "/api/v1/admin/boundaries",
		},
	})
}

// GetBoundaryLoadsHandler lists every layer load and how it went.
//
// GET /api/v1/admin/boundaries
func GetBoundaryLoadsHandler(c echo.Context) error {
	loads, err := services.ListBoundaryLoads(services.GetDB())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, GeocodeResponse{Success: false, Error: err.Error()})
	}
	return c.JSON(http.StatusOK, GeocodeResponse{Success: true, Data: loads})
}
