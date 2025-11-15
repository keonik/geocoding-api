package models

// City represents a US city with location and demographic data
type City struct {
	ID           int64   `json:"id"`
	City         string  `json:"city"`
	CityAscii    string  `json:"city_ascii"`
	StateID      string  `json:"state_id"`
	StateName    string  `json:"state_name"`
	CountyFIPS   string  `json:"county_fips,omitempty"`
	CountyName   string  `json:"county_name,omitempty"`
	Lat          float64 `json:"lat"`
	Lng          float64 `json:"lng"`
	Population   int     `json:"population,omitempty"`
	Density      float64 `json:"density,omitempty"`
	Source       string  `json:"source,omitempty"`
	Military     bool    `json:"military"`
	Incorporated bool    `json:"incorporated"`
	Timezone     string  `json:"timezone,omitempty"`
	Ranking      int     `json:"ranking,omitempty"`
	Zips         string  `json:"zips,omitempty"`
	ExternalID   string  `json:"external_id,omitempty"`
}

// CitySearchParams represents search parameters for city lookups
type CitySearchParams struct {
	Query      string  `json:"query"`
	City       string  `json:"city"`
	State      string  `json:"state"`
	County     string  `json:"county"`
	Lat        float64 `json:"lat"`
	Lng        float64 `json:"lng"`
	Radius     float64 `json:"radius"`
	MinPop     int     `json:"min_population"`
	Limit      int     `json:"limit"`
	Offset     int     `json:"offset"`
}

// CitySearchResponse represents the response for city search requests
type CitySearchResponse struct {
	Success bool                   `json:"success"`
	Data    []City                 `json:"data,omitempty"`
	Count   int                    `json:"count,omitempty"`
	Total   int                    `json:"total,omitempty"`
	Query   string                 `json:"query,omitempty"`
	Filters map[string]interface{} `json:"filters,omitempty"`
	Message string                 `json:"message,omitempty"`
	Error   string                 `json:"error,omitempty"`
}
