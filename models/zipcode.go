package models

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ZipCode represents a US ZIP code with all geographical and administrative data
type ZipCode struct {
	ZipCode             string         `json:"zip_code" db:"zip_code"`
	CityName            string         `json:"city_name" db:"city_name"`
	StateCode           string         `json:"state_code" db:"state_code"`
	StateName           string         `json:"state_name" db:"state_name"`
	ZCTA                bool           `json:"zcta" db:"zcta"`
	ZCTAParent          *string        `json:"zcta_parent" db:"zcta_parent"`
	Population          *float64       `json:"population" db:"population"`
	Density             *float64       `json:"density" db:"density"`
	PrimaryCountyCode   string         `json:"primary_county_code" db:"primary_county_code"`
	PrimaryCountyName   string         `json:"primary_county_name" db:"primary_county_name"`
	CountyWeights       CountyWeights  `json:"county_weights" db:"county_weights"`
	CountyNames         StringArray    `json:"county_names" db:"county_names"`
	CountyCodes         StringArray    `json:"county_codes" db:"county_codes"`
	Imprecise           bool           `json:"imprecise" db:"imprecise"`
	Military            bool           `json:"military" db:"military"`
	Timezone            string         `json:"timezone" db:"timezone"`
	Latitude            float64        `json:"latitude" db:"latitude"`
	Longitude           float64        `json:"longitude" db:"longitude"`
}

// CountyWeights represents the JSON structure for county weights
type CountyWeights map[string]string

// StringArray represents an array of strings for database storage
type StringArray []string

// Value implements the driver.Valuer interface for CountyWeights
func (cw CountyWeights) Value() (driver.Value, error) {
	if cw == nil {
		return nil, nil
	}
	return json.Marshal(cw)
}

// Scan implements the sql.Scanner interface for CountyWeights
func (cw *CountyWeights) Scan(value interface{}) error {
	if value == nil {
		*cw = make(CountyWeights)
		return nil
	}
	
	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("type assertion to []byte failed")
	}
	
	return json.Unmarshal(bytes, cw)
}

// Value implements the driver.Valuer interface for StringArray
func (sa StringArray) Value() (driver.Value, error) {
	if sa == nil {
		return nil, nil
	}
	return strings.Join(sa, ","), nil
}

// Scan implements the sql.Scanner interface for StringArray
func (sa *StringArray) Scan(value interface{}) error {
	if value == nil {
		*sa = StringArray{}
		return nil
	}
	
	str, ok := value.(string)
	if !ok {
		return errors.New("type assertion to string failed")
	}
	
	if str == "" {
		*sa = StringArray{}
		return nil
	}
	
	*sa = strings.Split(str, ",")
	return nil
}

// ParseGeoPoint parses the "latitude, longitude" format from CSV
func ParseGeoPoint(geoPoint string) (latitude, longitude float64, err error) {
	parts := strings.Split(strings.TrimSpace(geoPoint), ",")
	if len(parts) != 2 {
		return 0, 0, errors.New("invalid geo point format")
	}
	
	latitude, err = strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil {
		return 0, 0, err
	}
	
	longitude, err = strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err != nil {
		return 0, 0, err
	}
	
	return latitude, longitude, nil
}

// ParseCountyWeights parses the JSON string from CSV
func ParseCountyWeights(weightsStr string) (CountyWeights, error) {
	// Remove outer quotes and unescape inner quotes
	weightsStr = strings.Trim(weightsStr, "\"")
	weightsStr = strings.ReplaceAll(weightsStr, "\"\"", "\"")
	
	// First try to unmarshal as map[string]string
	var weights CountyWeights
	err := json.Unmarshal([]byte(weightsStr), &weights)
	if err == nil {
		return weights, nil
	}
	
	// If that fails, try as map[string]interface{} and convert values to strings
	var rawWeights map[string]interface{}
	err = json.Unmarshal([]byte(weightsStr), &rawWeights)
	if err != nil {
		return nil, err
	}
	
	// Convert all values to strings
	weights = make(CountyWeights)
	for key, value := range rawWeights {
		weights[key] = fmt.Sprintf("%v", value)
	}
	
	return weights, nil
}

// ParseStringArray parses comma-separated string into StringArray
func ParseStringArray(str string) StringArray {
	if str == "" {
		return StringArray{}
	}
	return strings.Split(str, ",")
}