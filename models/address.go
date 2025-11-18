package models

import (
	"time"
)

// OhioAddress represents an address record from Ohio counties
type OhioAddress struct {
	ID           int64     `json:"id" db:"id"`
	Hash         string    `json:"hash" db:"hash"`
	HouseNumber  string    `json:"house_number" db:"house_number"`
	Street       string    `json:"street" db:"street"`
	Unit         string    `json:"unit" db:"unit"`
	City         string    `json:"city" db:"city"`
	District     string    `json:"district" db:"district"` // County abbreviation
	Region       string    `json:"region" db:"region"`     // State code
	Postcode     string    `json:"postcode" db:"postcode"`
	County       string    `json:"county" db:"county"`     // Full county name
	FullAddress  string    `json:"full_address" db:"full_address"` // Complete formatted address
	Latitude     float64   `json:"latitude" db:"latitude"`
	Longitude    float64   `json:"longitude" db:"longitude"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
}

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
	Limit    int     `json:"limit" form:"limit"`       // Number of results to return (default: 50, max: 500)
	Offset   int     `json:"offset" form:"offset"`     // Offset for pagination
}

// AddressSearchResponse represents the response for address search
type AddressSearchResponse struct {
	Success   bool            `json:"success"`
	Data      []OhioAddress   `json:"data"`
	Count     int             `json:"count"`
	Total     int             `json:"total,omitempty"`
	Error     string          `json:"error,omitempty"`
	Query     string          `json:"query,omitempty"`
	Filters   map[string]any  `json:"filters,omitempty"`
}