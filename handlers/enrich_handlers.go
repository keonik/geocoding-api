package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"geocoding-api/services"

	"github.com/labstack/echo/v4"
)

// parseLatLng reads and range-checks the lat and lng query parameters. A
// non-empty problem is the caller's mistake, for a 400; with the range checked
// here, any error from the lookup that follows is the server's.
func parseLatLng(c echo.Context) (lat, lng float64, problem string) {
	latRaw, lngRaw := c.QueryParam("lat"), c.QueryParam("lng")
	if latRaw == "" || lngRaw == "" {
		return 0, 0, "Both lat and lng are required"
	}
	lat, err := strconv.ParseFloat(latRaw, 64)
	if err != nil || math.IsNaN(lat) {
		return 0, 0, "lat is not a number"
	}
	lng, err = strconv.ParseFloat(lngRaw, 64)
	if err != nil || math.IsNaN(lng) {
		return 0, 0, "lng is not a number"
	}
	// Rejected rather than clamped: a caller who passes them the wrong way
	// round gets told, instead of a confident answer about somewhere else.
	if lat < -90 || lat > 90 {
		return 0, 0, fmt.Sprintf("lat %g is outside -90..90", lat)
	}
	if lng < -180 || lng > 180 {
		return 0, 0, fmt.Sprintf("lng %g is outside -180..180", lng)
	}
	return lat, lng, ""
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
	lat, lng, problem := parseLatLng(c)
	if problem != "" {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{
			Success: false,
			Error:   problem,
			Data:    map[string]interface{}{"example": "/api/v1/enrich?lat=39.9612&lng=-83.0007&fields=census,cd"},
		})
	}
	req, err := services.ParseEnrichmentFields(c.QueryParam("fields"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{Success: false, Error: err.Error()})
	}

	result, err := services.Enrich(services.GetDB(), lat, lng, req)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, GeocodeResponse{Success: false, Error: err.Error()})
	}
	return c.JSON(http.StatusOK, GeocodeResponse{
		Success: true,
		Data:    EnrichResponse{Lat: lat, Lng: lng, Enrichment: result},
	})
}

// LoadBoundariesRequest names what to load. An empty layers list means
// services.DefaultBoundaryLayers.
type LoadBoundariesRequest struct {
	State  string   `json:"state"`
	Layers []string `json:"layers"`
}

// boundaryLoadTimeout bounds one layer. Ohio's blocks, the largest file, take
// under two minutes; this is generous without letting a stalled download hold
// its claim forever. It must stay under services.staleLoadAfter, or a live
// load's claim could be taken over.
const boundaryLoadTimeout = 30 * time.Minute

// LoadBoundariesHandler starts loading Census boundary layers for a state.
//
// Each layer loads in its own goroutine after the response: a state's block
// groups alone are a 19 MB download, longer than a proxy will hold a request
// open, and running them one after another would leave later claims aging in
// a queue. Progress is read from GET /admin/boundaries.
//
// POST /api/v1/admin/boundaries/load {"state": "OH", "layers": ["tract", "congressional_district"]}
func LoadBoundariesHandler(c echo.Context) error {
	var req LoadBoundariesRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, GeocodeResponse{Success: false, Error: "Malformed body"})
	}
	db := services.GetDB()

	layers := services.DefaultBoundaryLayers()
	if len(req.Layers) > 0 {
		layers = nil
		for _, name := range req.Layers {
			l, ok := services.BoundaryLayerByName(name)
			if !ok {
				return c.JSON(http.StatusBadRequest, GeocodeResponse{Success: false, Error: "unknown layer " + strconv.Quote(name)})
			}
			layers = append(layers, l)
		}
	}

	// A national layer covers the country in one file, so it needs no state.
	// Asking for one alongside per-state layers is fine; the state applies to
	// those, and the national layer ignores it.
	perState := false
	for _, l := range layers {
		if !l.IsNational() {
			perState = true
		}
	}
	fips := services.NationalScope
	if perState || strings.TrimSpace(req.State) != "" {
		var err error
		if fips, err = services.ResolveStateFIPS(db, req.State); err != nil {
			return c.JSON(http.StatusBadRequest, GeocodeResponse{Success: false, Error: err.Error()})
		}
	}

	// Every layer is claimed and, if claimed, started. A failure to claim one
	// is reported beside the others rather than aborting the request, which
	// would leave the layers already claimed marked loading with nothing
	// running until their claims went stale.
	started, busy := []string{}, []string{}
	failed := map[string]string{}
	for _, l := range layers {
		claim, err := services.BeginBoundaryLoad(db, l, fips)
		switch {
		case errors.Is(err, services.ErrBoundaryLoadInProgress):
			busy = append(busy, l.Name)
		case err != nil:
			failed[l.Name] = err.Error()
		default:
			started = append(started, l.Name)
			go runBoundaryLoad(db, claim)
		}
	}

	status := http.StatusAccepted
	if len(started) == 0 && len(failed) > 0 {
		status = http.StatusInternalServerError
	}
	return c.JSON(status, GeocodeResponse{
		Success: len(failed) == 0,
		Data: map[string]interface{}{
			"state_fips":          fips,
			"started":             started,
			"already_in_progress": busy,
			"failed_to_start":     failed,
			"status":              "/api/v1/admin/boundaries",
		},
	})
}

// runBoundaryLoad runs one claimed load. Load records its own outcome,
// panics included; this only logs it, and guards the process against
// anything that escapes, since echo's recovery does not reach a goroutine.
func runBoundaryLoad(db *sql.DB, claim *services.BoundaryClaim) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("boundary load %s/%s panicked: %v", claim.Layer.Name, claim.StateFIPS, r)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), boundaryLoadTimeout)
	defer cancel()
	n, err := claim.Load(ctx, db)
	if err != nil {
		log.Printf("boundary load %s/%s failed: %v", claim.Layer.Name, claim.StateFIPS, err)
		return
	}
	log.Printf("boundary load %s/%s: %d features", claim.Layer.Name, claim.StateFIPS, n)
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
