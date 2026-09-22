package services

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/lib/pq"
)

// EnrichmentGroups are the values fields= accepts, in the order they are
// documented. Each selects boundary layers, except timezone.
var EnrichmentGroups = []string{"census", "cd", "stateleg", "school", "place", "timezone"}

// enrichmentSource names where the boundaries come from. Tract, block group
// and block codes within it are 2020-Census geographies, which the Census
// keeps until 2030; districts are as of the release.
const enrichmentSource = "Census TIGER/Line 2025"

// EnrichmentRequest is what fields= asked for.
type EnrichmentRequest struct {
	Layers   []BoundaryLayer
	Timezone bool
}

// Boundary is the polygon of one layer that contains a point.
type Boundary struct {
	GEOID      string            `json:"geoid"`
	Name       string            `json:"name"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// Enrichment answers "which districts is this point in".
//
// Boundaries has an entry for every requested layer. A null entry means the
// layer is loaded for this state and the point is in none of its polygons --
// a point in a lake is in no tract, and Ohio has no elementary school
// districts at all. A layer that has not been loaded for the state is listed
// in Unavailable instead, and has no entry: "we do not know" is not the same
// answer as "there is none", and a caller must not read one as the other.
type Enrichment struct {
	StateFIPS   *string              `json:"state_fips"`
	Source      string               `json:"source"`
	Boundaries  map[string]*Boundary `json:"boundaries"`
	Unavailable []string             `json:"unavailable"`

	// Timezone is present when fields= asked for it and it is known, with
	// TimezoneSource saying which answer it is: the exact zone polygon, or
	// the nearest ZIP's zone when those are not loaded. Asked for and not
	// known, it is listed in Unavailable, like a layer.
	Timezone       *string `json:"timezone,omitempty"`
	TimezoneSource string  `json:"timezone_source,omitempty"`
}

// ParseEnrichmentFields turns "census,cd" into a request. An empty string
// means every group.
func ParseEnrichmentFields(raw string) (EnrichmentRequest, error) {
	want := map[string]bool{}
	for _, f := range strings.Split(raw, ",") {
		if f = strings.ToLower(strings.TrimSpace(f)); f != "" {
			want[f] = true
		}
	}
	known := map[string]bool{}
	for _, g := range EnrichmentGroups {
		known[g] = true
	}
	var unknown []string
	for f := range want {
		if !known[f] {
			unknown = append(unknown, f)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return EnrichmentRequest{}, fmt.Errorf("unknown fields %s; valid fields are %s",
			strings.Join(unknown, ", "), strings.Join(EnrichmentGroups, ", "))
	}

	req := EnrichmentRequest{Timezone: len(want) == 0 || want["timezone"]}
	for _, l := range BoundaryLayers {
		if l.AnswersAsTimezone() {
			continue
		}
		if len(want) == 0 || want[l.Group] {
			req.Layers = append(req.Layers, l)
		}
	}
	return req, nil
}

// Enrich finds the polygon of each layer that contains the point.
//
// Three queries: the containing state, what has been loaded for it, and one
// GIST probe for every layer at once. A point in no state is outside the US
// -- TIGER state polygons include their coastal and Great Lakes water -- so
// every layer comes back null and none unavailable.
func Enrich(db *sql.DB, lat, lng float64, req EnrichmentRequest) (*Enrichment, error) {
	if lat < -90 || lat > 90 {
		return nil, fmt.Errorf("latitude %g is outside -90..90", lat)
	}
	if lng < -180 || lng > 180 {
		return nil, fmt.Errorf("longitude %g is outside -180..180", lng)
	}

	e := &Enrichment{Source: enrichmentSource, Boundaries: map[string]*Boundary{}, Unavailable: []string{}}
	names := make([]string, len(req.Layers))
	for i, l := range req.Layers {
		names[i] = l.Name
	}

	var fips, abbr string
	err := db.QueryRow(`
		SELECT state_fips, state_abbr FROM us_states
		WHERE geometry IS NOT NULL
		  AND ST_Covers(geometry, ST_SetSRID(ST_MakePoint($1, $2), 4326))
		ORDER BY state_fips
		LIMIT 1
	`, lng, lat).Scan(&fips, &abbr)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to find the containing state: %w", err)
	}
	if req.Timezone {
		if e.Timezone, e.TimezoneSource, err = timezoneAtPoint(db, lat, lng, abbr); err != nil {
			return nil, err
		}
		if e.Timezone == nil {
			e.Unavailable = append(e.Unavailable, "timezone")
		}
	}
	if fips == "" {
		for _, n := range names {
			e.Boundaries[n] = nil
		}
		return e, nil
	}
	e.StateFIPS = &fips

	loaded := map[string]bool{}
	rows, err := db.Query(`
		SELECT layer FROM boundary_loads
		WHERE state_fips = $1 AND layer = ANY($2) AND available
	`, fips, pq.Array(names))
	if err != nil {
		if isUndefinedTable(err) {
			// Migration 26 pending: nothing is loaded yet, which is the
			// truthful answer for every layer.
			e.Unavailable = names
			return e, nil
		}
		return nil, fmt.Errorf("failed to read loaded layers: %w", err)
	}
	for rows.Next() {
		var layer string
		if err := rows.Scan(&layer); err != nil {
			rows.Close()
			return nil, fmt.Errorf("failed to read loaded layers: %w", err)
		}
		loaded[layer] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read loaded layers: %w", err)
	}
	for _, n := range names {
		if loaded[n] {
			e.Boundaries[n] = nil
		} else {
			e.Unavailable = append(e.Unavailable, n)
		}
	}

	// A point exactly on a shared edge is covered by both neighbours. The
	// lowest GEOID wins so the answer is the same every time.
	rows, err = db.Query(`
		SELECT DISTINCT ON (layer) layer, geoid, name, attrs
		FROM boundaries
		WHERE layer = ANY($1)
		  AND ST_Covers(geom, ST_SetSRID(ST_MakePoint($2, $3), 4326))
		ORDER BY layer, geoid
	`, pq.Array(names), lng, lat)
	if err != nil {
		return nil, fmt.Errorf("failed to look up boundaries: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var layer string
		var b Boundary
		var attrs []byte
		if err := rows.Scan(&layer, &b.GEOID, &b.Name, &attrs); err != nil {
			return nil, fmt.Errorf("failed to read a boundary: %w", err)
		}
		if err := json.Unmarshal(attrs, &b.Attributes); err != nil {
			return nil, fmt.Errorf("failed to read %s attributes: %w", layer, err)
		}
		// Only layers judged available get an answer. Rows for a state
		// whose first load is still running are not yet evidence.
		if _, ok := e.Boundaries[layer]; ok {
			b := b
			e.Boundaries[layer] = &b
		}
	}
	return e, rows.Err()
}
