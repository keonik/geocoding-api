package services

import (
	"database/sql"
	"fmt"
	"strings"

	"geocoding-api/models"
)

// MaxBatchItems caps one request.
//
// Every item is a lookup, so an unbounded batch is an unbounded query. 100 is
// large enough to be worth the round trip and small enough that a request stays
// interactive; anything bigger belongs in an async job rather than a longer
// timeout.
const MaxBatchItems = 100

// BatchItem is one lookup. Exactly one of ZipCode or Query identifies it.
type BatchItem struct {
	// ID is echoed back untouched so a caller can match results to inputs
	// without relying on ordering. Optional.
	ID      string `json:"id,omitempty"`
	ZipCode string `json:"zip_code,omitempty"`
	Query   string `json:"query,omitempty"`
}

// BatchResult is one answer, in the same position as its item.
//
// Found is explicit rather than implied by a nil result: "no match" and "this
// failed" are different outcomes and a caller needs to tell them apart.
type BatchResult struct {
	ID      string              `json:"id,omitempty"`
	Found   bool                `json:"found"`
	Error   string              `json:"error,omitempty"`
	ZipCode *models.ZipCode     `json:"zip_code,omitempty"`
	Address *models.OhioAddress `json:"address,omitempty"`
}

// BatchResponse carries the results and what the request cost.
type BatchResponse struct {
	Results []BatchResult `json:"results"`
	Total   int           `json:"total"`
	Found   int           `json:"found"`
	// BillableUnits is what this request counted against the caller's quota:
	// one per item, not one per request. Stated in the response because a
	// caller should not have to infer how they are being charged.
	BillableUnits int `json:"billable_units"`
}

// BatchGeocode resolves many lookups in one request.
//
// The plans have advertised "bulk" as a pro and enterprise feature since before
// any of this existed (models/auth.go), with no endpoint behind it.
//
// ZIP lookups are resolved with a single query over the whole batch rather than
// one per item -- the entire point of a batch endpoint is the round trips it
// saves, and issuing N queries internally gives that back.
func BatchGeocode(db *sql.DB, items []BatchItem) (*BatchResponse, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("batch is empty")
	}
	if len(items) > MaxBatchItems {
		return nil, fmt.Errorf("batch has %d items, more than the %d allowed", len(items), MaxBatchItems)
	}

	resp := &BatchResponse{
		Results:       make([]BatchResult, len(items)),
		Total:         len(items),
		BillableUnits: len(items),
	}

	// Collect the ZIP lookups so they can be answered together.
	zipWanted := make(map[string][]int)
	for i, item := range items {
		resp.Results[i].ID = item.ID

		zip := strings.TrimSpace(item.ZipCode)
		query := strings.TrimSpace(item.Query)

		switch {
		case zip != "" && query != "":
			resp.Results[i].Error = "provide either zip_code or query, not both"
		case zip != "":
			zipWanted[zip] = append(zipWanted[zip], i)
		case query != "":
			// Address search runs per item: each one is a different full-text
			// query, so there is nothing to combine.
			addresses, _, err := addressServiceFor(db).SearchAddresses(models.AddressSearchParams{
				Query: query, Limit: 1,
			})
			if err != nil {
				resp.Results[i].Error = "lookup failed"
				continue
			}
			if len(addresses) > 0 {
				resp.Results[i].Found = true
				resp.Results[i].Address = &addresses[0]
			}
		default:
			resp.Results[i].Error = "item has neither zip_code nor query"
		}
	}

	if len(zipWanted) > 0 {
		if err := resolveZipBatch(db, zipWanted, resp); err != nil {
			return nil, err
		}
	}

	for _, r := range resp.Results {
		if r.Found {
			resp.Found++
		}
	}
	return resp, nil
}

// resolveZipBatch answers every ZIP in the request with one query.
func resolveZipBatch(db *sql.DB, wanted map[string][]int, resp *BatchResponse) error {
	codes := make([]string, 0, len(wanted))
	for code := range wanted {
		codes = append(codes, code)
	}

	placeholders := make([]string, len(codes))
	args := make([]interface{}, len(codes))
	for i, code := range codes {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = code
	}

	rows, err := db.Query(`
		SELECT zip_code, city_name, state_code, state_name, zcta, zcta_parent,
		       population, density, primary_county_code, primary_county_name,
		       county_weights, county_names, county_codes, imprecise, military,
		       timezone, latitude, longitude
		FROM zip_codes
		WHERE zip_code IN (`+strings.Join(placeholders, ", ")+`)`, args...)
	if err != nil {
		return fmt.Errorf("failed to look up ZIP codes: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var z models.ZipCode
		if err := rows.Scan(
			&z.ZipCode, &z.CityName, &z.StateCode, &z.StateName, &z.ZCTA, &z.ZCTAParent,
			&z.Population, &z.Density, &z.PrimaryCountyCode, &z.PrimaryCountyName,
			&z.CountyWeights, &z.CountyNames, &z.CountyCodes, &z.Imprecise, &z.Military,
			&z.Timezone, &z.Latitude, &z.Longitude,
		); err != nil {
			return fmt.Errorf("failed to read ZIP code: %w", err)
		}

		found := z
		// One ZIP repeated across items is one row here and every position it
		// was asked for gets the answer.
		for _, idx := range wanted[z.ZipCode] {
			resp.Results[idx].Found = true
			resp.Results[idx].ZipCode = &found
		}
	}
	return rows.Err()
}

func addressServiceFor(db *sql.DB) *AddressService {
	if db == nil {
		return NewAddressService(GetDB())
	}
	return NewAddressService(db)
}
