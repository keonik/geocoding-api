package services

import (
	"database/sql"
	"strings"
	"sync"
	"time"

	"geocoding-api/database"
	"geocoding-api/utils"
)

// dataQualityTTL bounds how often the scan runs.
//
// Every check below is an aggregate over the whole address table, which at ~5.8M
// rows is seconds of work. Nothing here changes except when a dataset is
// imported, so a cached answer is never far wrong and an operator refreshing a
// dashboard does not re-scan the table each time.
const dataQualityTTL = 30 * time.Minute

// usBounds is a generous envelope around the United States, used only to catch
// coordinates that are obviously wrong -- a zero pair, a sign flip, a
// transposed lat/lng. It is deliberately loose: the point is to find nonsense,
// not to police borders.
const (
	usMinLng, usMinLat = -180.0, 15.0
	usMaxLng, usMaxLat = -60.0, 72.0
)

// RegionIssue is one region code that is not a valid two-letter US state.
type RegionIssue struct {
	Region    string `json:"region"`
	Addresses int    `json:"addresses"`
	Reason    string `json:"reason"`
}

// DataQuality reports the silent correctness problems in the address table.
//
// Every field here describes something that is wrong but does not raise an
// error anywhere: a search still returns rows, an import still reports success,
// and nothing in the logs says otherwise. Production held eleven distinct
// region codes -- including ON (Ontario), BE, IH, PJ and a bare 0 -- and the
// only reason anyone noticed was an unrelated endpoint happening to group by
// region. That is the gap this closes.
type DataQuality struct {
	TotalAddresses int `json:"total_addresses"`

	// InvalidRegions are region codes that are not real US states. They come
	// from the legacy loader truncating a state name to two characters, and
	// each one is its own address-uniqueness bucket.
	InvalidRegions []RegionIssue `json:"invalid_regions"`

	// BlankRegion counts rows with no state at all. These share a single
	// uniqueness bucket, so two identical addresses in different states
	// silently collapse into one.
	BlankRegion int `json:"blank_region"`

	BlankCounty   int `json:"blank_county"`
	BlankCity     int `json:"blank_city"`
	BlankPostcode int `json:"blank_postcode"`

	// OutsideUS counts coordinates outside a loose envelope around the country
	// -- a zero pair, a dropped minus sign, a transposed lat/lng.
	OutsideUS int `json:"outside_us_bounds"`

	GeneratedAt time.Time `json:"generated_at"`
	MaxAgeSecs  int       `json:"max_age_seconds"`
}

type dataQualityCache struct {
	mu       sync.Mutex
	snapshot *DataQuality
	builtAt  time.Time
}

var dataQuality = &dataQualityCache{}

// GetDataQuality returns the current report, rescanning when stale.
func GetDataQuality(db *sql.DB) (*DataQuality, error) {
	dataQuality.mu.Lock()
	defer dataQuality.mu.Unlock()

	if dataQuality.snapshot != nil && time.Since(dataQuality.builtAt) < dataQualityTTL {
		return dataQuality.snapshot, nil
	}

	report, err := buildDataQuality(db)
	if err != nil {
		return nil, err
	}

	dataQuality.snapshot = report
	dataQuality.builtAt = time.Now()
	return report, nil
}

func buildDataQuality(db *sql.DB) (*DataQuality, error) {
	if db == nil {
		db = database.DB
	}

	report := &DataQuality{
		InvalidRegions: []RegionIssue{},
		GeneratedAt:    time.Now(),
		MaxAgeSecs:     int(dataQualityTTL.Seconds()),
	}

	// One pass for the counts. FILTER lets every check share a single scan
	// rather than each walking the table on its own.
	err := db.QueryRow(`
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE region IS NULL OR region = ''),
			COUNT(*) FILTER (WHERE county IS NULL OR county = ''),
			COUNT(*) FILTER (WHERE city IS NULL OR city = ''),
			COUNT(*) FILTER (WHERE postcode IS NULL OR postcode = ''),
			COUNT(*) FILTER (WHERE NOT ST_Intersects(
				geom, ST_MakeEnvelope($1, $2, $3, $4, 4326)))
		FROM ohio_addresses
	`, usMinLng, usMinLat, usMaxLng, usMaxLat).Scan(
		&report.TotalAddresses,
		&report.BlankRegion,
		&report.BlankCounty,
		&report.BlankCity,
		&report.BlankPostcode,
		&report.OutsideUS,
	)
	if err != nil {
		return nil, err
	}

	// Regions are classified in Go rather than SQL. The set of valid state
	// codes already lives in utils.IsUSStateCode, and embedding a second copy
	// in a query is how the two drift apart.
	rows, err := db.Query(`
		SELECT region, COUNT(*)
		FROM ohio_addresses
		WHERE region IS NOT NULL AND region <> ''
		GROUP BY region
		ORDER BY COUNT(*) DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var region string
		var n int
		if err := rows.Scan(&region, &n); err != nil {
			return nil, err
		}

		// IsUSStateCode upper-cases before checking, so it accepts "oh" -- which
		// is the point of the first arm below: a value is only correct if it is
		// a real code AND already in the canonical case. 'oh' and 'OH' are
		// distinct keys to the uniqueness index, so a case difference is a real
		// defect, not a cosmetic one.
		switch {
		case region == strings.ToUpper(region) && utils.IsUSStateCode(region):
			// Correct, nothing to report.
		case utils.IsUSStateCode(region):
			report.InvalidRegions = append(report.InvalidRegions, RegionIssue{
				Region: region, Addresses: n,
				Reason: "valid state code in the wrong case; it is a separate uniqueness bucket from the upper-case form",
			})
		case len(region) < 2:
			report.InvalidRegions = append(report.InvalidRegions, RegionIssue{
				Region: region, Addresses: n,
				Reason: "too short to be a state code, most likely a truncated state name",
			})
		default:
			report.InvalidRegions = append(report.InvalidRegions, RegionIssue{
				Region: region, Addresses: n,
				Reason: "not a US state code; the legacy loader truncated state names to two characters",
			})
		}
	}

	return report, rows.Err()
}

// ResetDataQualityCache drops the cached report, so a fresh scan runs after an
// import rather than serving a pre-import picture.
func ResetDataQualityCache() {
	dataQuality.mu.Lock()
	defer dataQuality.mu.Unlock()
	dataQuality.snapshot = nil
}
