package models

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// OhioAddress represents an address record from Ohio counties
type OhioAddress struct {
	ID          int64     `json:"id" db:"id"`
	Hash        string    `json:"hash" db:"hash"`
	HouseNumber string    `json:"house_number" db:"house_number"`
	Street      string    `json:"street" db:"street"`
	Unit        string    `json:"unit" db:"unit"`
	City        string    `json:"city" db:"city"`
	District    string    `json:"district" db:"district"` // County abbreviation
	Region      string    `json:"region" db:"region"`     // State code
	Postcode    string    `json:"postcode" db:"postcode"`
	County      string    `json:"county" db:"county"`             // Full county name
	FullAddress string    `json:"full_address" db:"full_address"` // Complete formatted address
	Latitude    float64   `json:"latitude" db:"latitude"`
	Longitude   float64   `json:"longitude" db:"longitude"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`

	// Match describes why this row was returned. Populated by the /addresses
	// search path; absent on a lookup by id, where there is nothing to have
	// matched, and absent on /addresses/search, which runs a different set of
	// passes that do not yet report a tier.
	Match *AddressMatch `json:"match,omitempty"`
}

// AddressMatch tells a caller how good a hit is, not just that it is a hit.
//
// Search already knew all of this -- which pass produced the row, and how well
// it scored -- and threw it away, returning a flat list in which a typo rescue
// that barely cleared the similarity threshold is indistinguishable from an
// address that matched every word exactly. A caller matching addresses
// automatically has to decide whether to accept a result, and that decision
// needs this.
type AddressMatch struct {
	// Tier is which pass produced the row:
	//
	//	prefix  the full-text prefix index matched every query word
	//	fuzzy   only the trigram fallback matched, so the query was misspelled
	//	        or truncated
	//	filter  no text query; the row matched structured filters alone
	//	none    a text query was supplied but yielded no usable search terms,
	//	        so nothing was matched on and these rows mean little
	Tier string `json:"tier"`

	// Confidence is 0..1 within the tier, and is absent when there is no text
	// query to score against. It is not comparable across tiers: a fuzzy 0.9
	// is a strong typo match, not a better answer than a prefix 0.7.
	Confidence *float64 `json:"confidence,omitempty"`
}

// Match tiers.
const (
	MatchTierPrefix = "prefix"
	MatchTierFuzzy  = "fuzzy"
	MatchTierFilter = "filter"
	MatchTierNone   = "none"
)

// AddressSearchParams represents search parameters for address queries
type AddressSearchParams struct {
	Query    string  `json:"query" form:"query"`       // General search query
	County   string  `json:"county" form:"county"`     // Filter by county
	City     string  `json:"city" form:"city"`         // Filter by city
	Postcode string  `json:"postcode" form:"postcode"` // Filter by postal code
	Street   string  `json:"street" form:"street"`     // Filter by street name
	Lat      float64 `json:"lat" form:"lat"`           // Latitude for proximity search
	Lng      float64 `json:"lng" form:"lng"`           // Longitude for proximity search
	Radius   float64 `json:"radius" form:"radius"`     // Radius in kilometers for proximity search

	// BBox restricts results to a rectangle, as minLng,minLat,maxLng,maxLat --
	// the order every mapping library emits, so a caller can pass a viewport
	// straight through without reordering it.
	BBox *BoundingBox `json:"bbox" form:"bbox"`

	// Polygon restricts results to an arbitrary shape, given as GeoJSON. A
	// radius is a circle and a bbox is a rectangle; a sales territory, a
	// delivery zone or a canvassing walk is neither.
	Polygon string `json:"polygon" form:"polygon"`
	Limit   int    `json:"limit" form:"limit"`   // Number of results to return (default: 50, max: 500)
	Offset  int    `json:"offset" form:"offset"` // Offset for pagination
}

// AddressSearchResponse represents the response for address search
type AddressSearchResponse struct {
	Success bool           `json:"success"`
	Data    []OhioAddress  `json:"data"`
	Count   int            `json:"count"`
	Total   int            `json:"total,omitempty"`
	Error   string         `json:"error,omitempty"`
	Query   string         `json:"query,omitempty"`
	Filters map[string]any `json:"filters,omitempty"`
}

// BoundingBox is a rectangle in WGS84 degrees.
type BoundingBox struct {
	MinLng float64 `json:"min_lng"`
	MinLat float64 `json:"min_lat"`
	MaxLng float64 `json:"max_lng"`
	MaxLat float64 `json:"max_lat"`
}

// ParseBBox reads "minLng,minLat,maxLng,maxLat".
//
// Ordering is longitude-first because that is what GeoJSON, Leaflet, MapLibre
// and PostGIS all use. Latitude-first is the common mistake and it is silent:
// a swapped pair inside Ohio's range still parses and just returns nothing, so
// the bounds are validated rather than trusted.
func ParseBBox(raw string) (*BoundingBox, error) {
	parts := strings.Split(raw, ",")
	if len(parts) != 4 {
		return nil, fmt.Errorf("bbox needs 4 comma-separated values (minLng,minLat,maxLng,maxLat), got %d", len(parts))
	}

	vals := make([]float64, 4)
	for i, p := range parts {
		v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			return nil, fmt.Errorf("bbox value %d is not a number: %q", i+1, strings.TrimSpace(p))
		}
		vals[i] = v
	}

	// ParseFloat accepts "NaN" and "Inf", and every comparison against NaN is
	// false -- so all three checks below pass and the value flows into
	// ST_MakeEnvelope, producing either an empty 200 or a PostGIS error. That
	// is exactly the silent-empty-result failure this validation exists to
	// stop, so it has to be rejected before the range checks, not by them.
	for i, v := range vals {
		if math.IsNaN(v) {
			return nil, fmt.Errorf("bbox value %d is NaN", i+1)
		}
		if math.IsInf(v, 0) {
			return nil, fmt.Errorf("bbox value %d is infinite", i+1)
		}
	}

	box := &BoundingBox{MinLng: vals[0], MinLat: vals[1], MaxLng: vals[2], MaxLat: vals[3]}

	if box.MinLng < -180 || box.MaxLng > 180 || box.MinLat < -90 || box.MaxLat > 90 {
		return nil, fmt.Errorf("bbox is outside valid coordinate ranges (longitude -180..180, latitude -90..90)")
	}
	if box.MinLng >= box.MaxLng {
		return nil, fmt.Errorf("bbox min longitude (%g) must be less than max longitude (%g); values are minLng,minLat,maxLng,maxLat", box.MinLng, box.MaxLng)
	}
	if box.MinLat >= box.MaxLat {
		return nil, fmt.Errorf("bbox min latitude (%g) must be less than max latitude (%g); values are minLng,minLat,maxLng,maxLat", box.MinLat, box.MaxLat)
	}

	return box, nil
}
