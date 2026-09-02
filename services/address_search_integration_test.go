package services

import (
	"database/sql"
	"os"
	"testing"

	"geocoding-api/models"

	_ "github.com/lib/pq"
)

// Temporary probe: exercises the Go query builder end-to-end against a real
// Postgres. Skipped unless PROBE_DSN is set.
func TestAddressSearchProbe(t *testing.T) {
	dsn := os.Getenv("PROBE_DSN")
	if dsn == "" {
		t.Skip("PROBE_DSN not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	svc := NewAddressService(db)

	cases := []struct {
		name   string
		params models.AddressSearchParams
		want   int
	}{
		{"one word exact", models.AddressSearchParams{Query: "BARENDT"}, 1},
		{"two words", models.AddressSearchParams{Query: "7057 barendt"}, 1},
		{"prefix", models.AddressSearchParams{Query: "bare"}, 1},
		{"no query", models.AddressSearchParams{}, 3},
		{"county filter", models.AddressSearchParams{Query: "main", County: "Franklin"}, 1},
		{"proximity+radius", models.AddressSearchParams{Query: "main", Lat: 39.96, Lng: -82.99, Radius: 5}, 1},
		{"proximity no radius", models.AddressSearchParams{Lat: 39.96, Lng: -82.99}, 3},
		{"misspelling -> trigram", models.AddressSearchParams{Query: "barendtt"}, 1},
		{"truncation -> trigram", models.AddressSearchParams{Query: "barend"}, 1},
		{"nonsense", models.AddressSearchParams{Query: "zzzzqqqq"}, 0},
	}

	for _, c := range cases {
		got, total, err := svc.SearchAddresses(c.params)
		if err != nil {
			t.Errorf("%-24s ERROR %v", c.name, err)
			continue
		}
		status := "ok "
		if total != c.want {
			status = "BAD"
			t.Errorf("%-24s total=%d want=%d", c.name, total, c.want)
		}
		t.Logf("  %s %-24s total=%d rows=%d", status, c.name, total, len(got))
	}
}
