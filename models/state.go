package models

import "time"

// State represents a US state with boundary geometry
type State struct {
	ID          int64     `json:"id"`
	StateFIPS   string    `json:"state_fips"`
	StateAbbr   string    `json:"state_abbr"`
	StateName   string    `json:"state_name"`
	StateNS     string    `json:"state_ns,omitempty"`
	GeoID       string    `json:"geoid,omitempty"`
	Region      string    `json:"region,omitempty"`
	Division    string    `json:"division,omitempty"`
	LSAD        string    `json:"lsad,omitempty"`
	MTFCC       string    `json:"mtfcc,omitempty"`
	FuncStat    string    `json:"funcstat,omitempty"`
	AreaLand    int64     `json:"area_land,omitempty"`
	AreaWater   int64     `json:"area_water,omitempty"`
	InternalLat float64   `json:"internal_lat,omitempty"`
	InternalLng float64   `json:"internal_lng,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// StateWithGeometry includes the full geometry for map rendering
type StateWithGeometry struct {
	State
	Geometry interface{} `json:"geometry"`
}

// StateSearchParams represents search parameters for states
type StateSearchParams struct {
	Name       string  `query:"name"`
	Abbr       string  `query:"abbr"`
	Region     string  `query:"region"`
	Division   string  `query:"division"`
	Lat        float64 `query:"lat"`
	Lng        float64 `query:"lng"`
	Limit      int     `query:"limit"`
	Offset     int     `query:"offset"`
}

// StateResponse wraps state data for API responses
type StateResponse struct {
	State    *State `json:"state"`
	GeoJSON  interface{} `json:"geojson,omitempty"`
}

// StateSearchResponse wraps search results
type StateSearchResponse struct {
	States []State `json:"states"`
	Total  int     `json:"total"`
	Limit  int     `json:"limit"`
	Offset int     `json:"offset"`
}
