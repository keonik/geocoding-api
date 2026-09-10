package services

import (
	"database/sql"
	"os"
	"testing"

	"geocoding-api/database"
)

// execMarker runs a marker statement through the same pool the service uses, so
// it bookends the rate-limit queries in the server log.
func execMarker(t *testing.T, query string) (sql.Result, error) {
	t.Helper()
	return database.DB.Exec(query)
}

// TestProbeRateLimitQueryCount exists to be run by hand with statement logging
// enabled on the probe database, so the round-trip count of the rate-limit path
// can be counted rather than asserted from reading the code. It emits a marker
// query on either side of a single CheckRateLimitStatus call.
//
// Skipped unless PROBE_QUERY_COUNT is set, because it is a measurement harness,
// not an assertion.
func TestProbeRateLimitQueryCount(t *testing.T) {
	if os.Getenv("PROBE_QUERY_COUNT") == "" {
		t.Skip("PROBE_QUERY_COUNT not set")
	}

	cleanup := setupRateLimitSchema(t)
	defer cleanup()

	userID := seedUser(t, "querycount@example.test", "free", false)
	seedSubscription(t, userID, "free", 3000)
	seedUsage(t, userID, 3)

	if _, err := execMarker(t, "SELECT 'RATELIMIT_PROBE_BEGIN'"); err != nil {
		t.Fatalf("marker failed: %v", err)
	}

	if _, err := Auth.CheckRateLimitStatus(userID); err != nil {
		t.Fatalf("CheckRateLimitStatus failed: %v", err)
	}

	if _, err := execMarker(t, "SELECT 'RATELIMIT_PROBE_END'"); err != nil {
		t.Fatalf("marker failed: %v", err)
	}
}
