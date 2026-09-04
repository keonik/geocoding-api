package models

import (
	"encoding/json"
	"time"
)

// OhioCounty represents a county in Ohio with boundary information
type OhioCounty struct {
	ID             int                    `json:"id" db:"id"`
	CountyName     string                 `json:"county_name" db:"county_name"`
	SourceName     string                 `json:"source_name" db:"source_name"`
	Layer          string                 `json:"layer" db:"layer"`
	AddressCount   int                    `json:"address_count" db:"address_count"`
	Stats          map[string]interface{} `json:"stats,omitempty" db:"stats"`
	BoundsGeometry string                 `json:"bounds_geometry,omitempty" db:"bounds_geometry"` // WKT format
	CreatedAt      time.Time              `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time              `json:"updated_at" db:"updated_at"`
}

// CountyBoundaryGeoJSON represents the GeoJSON format for county boundaries
type CountyBoundaryGeoJSON struct {
	Type     string                 `json:"type"`
	Features []CountyFeatureGeoJSON `json:"features"`
}

// CountyFeatureGeoJSON represents a single county feature in GeoJSON
type CountyFeatureGeoJSON struct {
	Type       string                  `json:"type"`
	Properties CountyPropertiesGeoJSON `json:"properties"`
	// Raw PostGIS ST_AsGeoJSON output, passed through untouched. Decoding it
	// into a Go structure only to re-encode it costs three passes over the
	// payload and one boxed float64 per coordinate, for no gain.
	Geometry json.RawMessage `json:"geometry"`
}

// CountyPropertiesGeoJSON represents the properties of a county feature
type CountyPropertiesGeoJSON struct {
	CountyName    string                 `json:"county_name"`
	SourceName    string                 `json:"source_name"`
	Layer         string                 `json:"layer"`
	AddressCount  int                    `json:"address_count"`
	Stats         map[string]interface{} `json:"stats,omitempty"`
}

// CountyListResponse represents a simplified list of counties
type CountyListResponse struct {
	ID           int    `json:"id"`
	CountyName   string `json:"county_name"`
	AddressCount int    `json:"address_count"`
}

// CountySearchParams represents parameters for searching counties
type CountySearchParams struct {
	Name         string `query:"name"`
	MinAddresses int    `query:"min_addresses"`
	MaxAddresses int    `query:"max_addresses"`
	Limit        int    `query:"limit"`
	Offset       int    `query:"offset"`
}