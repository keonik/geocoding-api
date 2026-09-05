package services

import (
	"database/sql"
	"os"
	"testing"

	"geocoding-api/database"

	_ "github.com/lib/pq"
)

// Exercises GetKeyUsage end-to-end against a real Postgres, including the
// scan targets -- the aggregate column types (int8, numeric, nullable
// timestamp) are where this query can fail without the SQL itself being
// wrong. Skipped unless PROBE_DSN is set.
func TestGetKeyUsageProbe(t *testing.T) {
	dsn := os.Getenv("PROBE_DSN")
	if dsn == "" {
		t.Skip("PROBE_DSN not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	prev := database.DB
	database.DB = db
	defer func() { database.DB = prev }()

	requireTables(t, db, "api_keys", "usage_records")

	as := &AuthService{}
	got, err := as.GetKeyUsage(1, 30)
	if err != nil {
		t.Fatalf("GetKeyUsage: %v", err)
	}

	// Four keys belong to user 1; user 2's key must never appear.
	if len(got) != 4 {
		t.Fatalf("want 4 keys for user 1, got %d: %+v", len(got), got)
	}
	for _, k := range got {
		if k.Name == "someone-else" {
			t.Fatalf("leaked another user's key: %+v", k)
		}
	}

	by := map[string]int{}
	for i, k := range got {
		by[k.Name] = i
	}

	cases := []struct {
		name         string
		calls        int
		billable     int
		errors       int
		active       bool
		wantLastCall bool
		wantPreview  string
	}{
		// 50 x 200 + 5 x 500, all billable
		{"production-web", 55, 55, 5, true, true, "gc_live_9f2"},
		// billable=false throughout, and key_preview is NULL -> COALESCE to ""
		{"batch-matcher", 20, 0, 0, true, true, ""},
		// revoked, and its history is all older than the window: must still
		// appear, with zeros and a null last_call
		{"retired-key", 0, 0, 0, false, false, "gc_live_old"},
		{"never-used", 0, 0, 0, true, false, "gc_live_new"},
	}

	for _, c := range cases {
		i, ok := by[c.name]
		if !ok {
			t.Errorf("%s: missing from results", c.name)
			continue
		}
		k := got[i]
		if k.TotalCalls != c.calls {
			t.Errorf("%s: total_calls = %d, want %d", c.name, k.TotalCalls, c.calls)
		}
		if k.BillableCalls != c.billable {
			t.Errorf("%s: billable_calls = %d, want %d", c.name, k.BillableCalls, c.billable)
		}
		if k.ErrorCount != c.errors {
			t.Errorf("%s: error_count = %d, want %d", c.name, k.ErrorCount, c.errors)
		}
		if k.IsActive != c.active {
			t.Errorf("%s: is_active = %v, want %v", c.name, k.IsActive, c.active)
		}
		if (k.LastCall != nil) != c.wantLastCall {
			t.Errorf("%s: last_call present = %v, want %v", c.name, k.LastCall != nil, c.wantLastCall)
		}
		if k.KeyPreview != c.wantPreview {
			t.Errorf("%s: key_preview = %q, want %q", c.name, k.KeyPreview, c.wantPreview)
		}
	}

	// avg over 50x10ms + 5x30ms = 650/55
	if k := got[by["production-web"]]; k.AvgResponseTime < 11.8 || k.AvgResponseTime > 11.9 {
		t.Errorf("production-web: avg_response_time = %v, want ~11.82", k.AvgResponseTime)
	}
	// Zero-call keys must report 0, not NaN or an error from a NULL AVG.
	if k := got[by["never-used"]]; k.AvgResponseTime != 0 {
		t.Errorf("never-used: avg_response_time = %v, want 0", k.AvgResponseTime)
	}
}
