package services

import (
	"database/sql"
	"sync"
	"time"

	"geocoding-api/database"
)

// coverageTTL is how long a computed snapshot is served before it is rebuilt.
//
// Coverage only changes when a dataset is imported, which is a deliberate
// admin action measured in weeks, so a stale answer is never far wrong. The
// TTL exists because the alternative is a GROUP BY over every address row on
// every request -- the same mistake the count query in address search was
// making.
const coverageTTL = 10 * time.Minute

// StateCoverage is what the service holds for one state.
type StateCoverage struct {
	State     string `json:"state"`
	Counties  int    `json:"counties"`
	Addresses int    `json:"addresses"`
}

// CountyCoverage is the per-county breakdown within a state.
type CountyCoverage struct {
	County    string `json:"county"`
	Addresses int    `json:"addresses"`
}

// Coverage answers "what data do you actually have".
//
// Without it a caller querying a state that was never loaded gets an empty
// result set, which is indistinguishable from a bad query or a broken service.
// That is a support ticket every time. The datasets table is admin-only and
// tracks uploads rather than contents, so it cannot answer this: the original
// Ohio import did not come through the uploader and would be missing entirely.
type Coverage struct {
	States      []StateCoverage `json:"states"`
	TotalRows   int             `json:"total_addresses"`
	GeneratedAt time.Time       `json:"generated_at"`
	MaxAgeSecs  int             `json:"max_age_seconds"`
}

type coverageCache struct {
	mu       sync.Mutex
	snapshot *Coverage
	builtAt  time.Time
}

var coverage = &coverageCache{}

// GetCoverage returns the current snapshot, recomputing it when stale.
//
// The lock is held across the query on purpose. A thundering herd of requests
// arriving on a cold cache would otherwise each run the aggregate; holding it
// means the first one pays and the rest wait for that result.
func GetCoverage(db *sql.DB) (*Coverage, error) {
	coverage.mu.Lock()
	defer coverage.mu.Unlock()

	if coverage.snapshot != nil && time.Since(coverage.builtAt) < coverageTTL {
		return coverage.snapshot, nil
	}

	snapshot, err := buildCoverage(db)
	if err != nil {
		// Serve a stale snapshot rather than an error. Coverage is
		// descriptive metadata; last week's answer is far more useful than a
		// 500, and it is never badly wrong.
		if coverage.snapshot != nil {
			return coverage.snapshot, nil
		}
		return nil, err
	}

	coverage.snapshot = snapshot
	coverage.builtAt = time.Now()
	return snapshot, nil
}

func buildCoverage(db *sql.DB) (*Coverage, error) {
	if db == nil {
		db = database.DB
	}

	rows, err := db.Query(`
		SELECT COALESCE(NULLIF(region, ''), 'OH') AS state,
		       COUNT(DISTINCT county) AS counties,
		       COUNT(*) AS addresses
		FROM ohio_addresses
		GROUP BY COALESCE(NULLIF(region, ''), 'OH')
		ORDER BY COUNT(*) DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	snapshot := &Coverage{
		States:      []StateCoverage{},
		GeneratedAt: time.Now(),
		MaxAgeSecs:  int(coverageTTL.Seconds()),
	}

	for rows.Next() {
		var sc StateCoverage
		if err := rows.Scan(&sc.State, &sc.Counties, &sc.Addresses); err != nil {
			return nil, err
		}
		snapshot.TotalRows += sc.Addresses
		snapshot.States = append(snapshot.States, sc)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return snapshot, nil
}

// GetStateCoverage returns the per-county breakdown for one state.
//
// Not cached: it is the drill-down, asked for far less often than the summary,
// and it is already narrowed by state.
func GetStateCoverage(db *sql.DB, state string) ([]CountyCoverage, error) {
	if db == nil {
		db = database.DB
	}

	rows, err := db.Query(`
		SELECT county, COUNT(*) AS addresses
		FROM ohio_addresses
		WHERE COALESCE(NULLIF(region, ''), 'OH') = UPPER($1)
		  AND county <> ''
		GROUP BY county
		ORDER BY county
	`, state)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counties := []CountyCoverage{}
	for rows.Next() {
		var cc CountyCoverage
		if err := rows.Scan(&cc.County, &cc.Addresses); err != nil {
			return nil, err
		}
		counties = append(counties, cc)
	}
	return counties, rows.Err()
}

// ResetCoverageCache drops the cached snapshot. Called after an import so the
// next request reflects newly loaded data rather than waiting out the TTL.
func ResetCoverageCache() {
	coverage.mu.Lock()
	defer coverage.mu.Unlock()
	coverage.snapshot = nil
}
