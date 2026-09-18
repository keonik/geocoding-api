package services

import (
	"database/sql"
	"fmt"
	"os"
	"testing"

	"geocoding-api/database"
	"geocoding-api/models"

	_ "github.com/lib/pq"
)

const citySchema = "city_probe"
const tiedCities = 120

// setupCityDB seeds cities that all tie on the sort key -- no ranking and no
// population -- which is the shape 77% of the real city file has. Probe-only
// search_path: nothing here uses PostGIS, so nothing needs public, and leaving
// it on lets a missing table read through to the real one.
func setupCityDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := os.Getenv("PROBE_DSN")
	if dsn == "" {
		t.Skip("PROBE_DSN not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		t.Skipf("probe database unreachable: %v", err)
	}

	for _, stmt := range []string{
		"DROP SCHEMA IF EXISTS " + citySchema + " CASCADE",
		"CREATE SCHEMA " + citySchema,
		"SET search_path TO " + citySchema,
		`CREATE TABLE cities (
			id BIGSERIAL PRIMARY KEY,
			city VARCHAR(255) NOT NULL, city_ascii VARCHAR(255) NOT NULL,
			state_id VARCHAR(2) NOT NULL, state_name VARCHAR(255) NOT NULL,
			county_fips VARCHAR(10), county_name VARCHAR(255),
			lat DECIMAL(10,7) NOT NULL, lng DECIMAL(11,7) NOT NULL,
			population INTEGER, density DECIMAL(10,2), source VARCHAR(50),
			military BOOLEAN DEFAULT FALSE, incorporated BOOLEAN DEFAULT FALSE,
			timezone VARCHAR(100), ranking INTEGER, zips TEXT, external_id VARCHAR(50),
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			CONSTRAINT unique_city_state UNIQUE (city_ascii, state_id))`,
		// Every row identical on ranking and population, so only id can order
		// them.
		fmt.Sprintf(`INSERT INTO cities (city, city_ascii, state_id, state_name, lat, lng, ranking, population)
			SELECT 'Town ' || i, 'Town ' || i, 'OH', 'Ohio', 40.0, -83.0, 0, NULL
			FROM generate_series(1, %d) i`, tiedCities),
		"ANALYZE cities",
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("setup failed on %.60q: %v", stmt, err)
		}
	}

	prev := database.DB
	database.DB = db
	t.Cleanup(func() {
		database.DB = prev
		if _, err := db.Exec("DROP SCHEMA IF EXISTS " + citySchema + " CASCADE"); err != nil {
			t.Logf("cleanup: %v", err)
		}
		db.Close()
	})
	return db
}

func walkCities(t *testing.T, base models.CitySearchParams, page int) []int64 {
	t.Helper()
	cs := &CityService{}
	var ids []int64
	for off := 0; ; off += page {
		p := base
		p.Limit, p.Offset = page, off
		rows, _, err := cs.SearchCities(p)
		if err != nil {
			t.Fatalf("search at offset %d: %v", off, err)
		}
		if len(rows) == 0 {
			break
		}
		for _, r := range rows {
			ids = append(ids, r.ID)
		}
		if off > tiedCities*4 {
			t.Fatal("pagination did not terminate")
		}
	}
	return ids
}

// 77% of the real city file ties on the sort key. With no tiebreaker a client
// paging through results saw some cities twice and never saw others.
func TestCityPaginationVisitsEveryCityOnce(t *testing.T) {
	setupCityDB(t)

	for _, tc := range []struct {
		name   string
		params models.CitySearchParams
	}{
		{"unfiltered", models.CitySearchParams{}},
		{"state filter", models.CitySearchParams{State: "OH"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ids := walkCities(t, tc.params, 10)

			seen := map[int64]int{}
			for _, id := range ids {
				seen[id]++
			}
			var dupes int
			for _, n := range seen {
				if n > 1 {
					dupes++
				}
			}
			if dupes > 0 {
				t.Errorf("%d cities appeared on more than one page", dupes)
			}
			if len(seen) != tiedCities {
				t.Errorf("walk covered %d distinct cities, want %d (%d skipped)",
					len(seen), tiedCities, tiedCities-len(seen))
			}
		})
	}
}

// The total has to match whichever path produced it: the separate count when
// unfiltered, the window when filtered.
func TestCityTotalsAreCorrectOnBothPaths(t *testing.T) {
	setupCityDB(t)
	cs := &CityService{}

	_, unfiltered, err := cs.SearchCities(models.CitySearchParams{Limit: 5})
	if err != nil {
		t.Fatalf("unfiltered: %v", err)
	}
	if unfiltered != tiedCities {
		t.Errorf("unfiltered total = %d, want %d", unfiltered, tiedCities)
	}

	_, filtered, err := cs.SearchCities(models.CitySearchParams{State: "OH", Limit: 5})
	if err != nil {
		t.Fatalf("filtered: %v", err)
	}
	if filtered != tiedCities {
		t.Errorf("filtered total = %d, want %d", filtered, tiedCities)
	}
}

// The window total rides on the rows, so a filtered page past the end carries
// none. Without the fallback a client one page too far reads total 0.
func TestCityTotalSurvivesAPagePastTheEnd(t *testing.T) {
	setupCityDB(t)
	cs := &CityService{}

	rows, total, err := cs.SearchCities(models.CitySearchParams{State: "OH", Limit: 10, Offset: tiedCities + 50})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected no rows past the end, got %d", len(rows))
	}
	if total != tiedCities {
		t.Errorf("total past the end = %d, want %d -- a client would think the results emptied", total, tiedCities)
	}
}
