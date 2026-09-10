package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"geocoding-api/database"
	"geocoding-api/models"
	"geocoding-api/services"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

// setupTestEnvironment initializes the database and services for testing.
//
// These tests need a seeded database; they are not self-contained. InitDB
// retries for sixty seconds by design, to tolerate container startup ordering
// in production, so without one this hung for the full timeout before failing
// -- which is why the package could not be part of a CI run. Probe the socket
// first and skip when nothing is listening.
func setupTestEnvironment(t *testing.T) {
	t.Helper()

	requireDatabase(t)

	// Initialize database
	if err := database.InitDB(); err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}

	// Initialize services
	services.InitAddressService(database.DB)
}

// requireDatabase skips unless the configured Postgres is actually reachable
// AND accepts our credentials. Shared by both setups in this package.
//
// A socket probe alone is not enough. Any other Postgres on port 5432 -- and
// a developer machine often has one for an unrelated project -- answers the
// dial, so the probe passes and InitDB then spends its full sixty-second
// retry budget failing to authenticate before the test finally errors out.
// Ping with a short deadline instead, and treat any failure as "not our
// database" rather than as a test failure.
func requireDatabase(t *testing.T) {
	t.Helper()
	host := envOr("DB_HOST", "localhost")
	port := envOr("DB_PORT", "5432")

	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 2*time.Second)
	if err != nil {
		t.Skipf("no database listening on %s:%s (these tests need a seeded database)", host, port)
	}
	conn.Close()

	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		host, port,
		envOr("DB_USER", "postgres"),
		envOr("DB_PASSWORD", "postgres"),
		envOr("DB_NAME", "geocoding_db"),
		envOr("DB_SSLMODE", "disable"),
	)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Skipf("cannot open %s:%s: %v", host, port, err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Skipf("database on %s:%s is not usable for these tests: %v", host, port, err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func TestSearchOhioAddresses(t *testing.T) {
	setupTestEnvironment(t)

	tests := []struct {
		name           string
		query          string
		county         string
		city           string
		street         string
		postcode       string
		expectedStatus int
		expectResults  bool
		description    string
	}{
		{
			name:           "Search by street address - Oakley",
			query:          "2525 Oakley",
			expectedStatus: http.StatusOK,
			expectResults:  true,
			description:    "Should find addresses on Oakley street",
		},
		{
			name:           "Search by street name only",
			street:         "Oakley",
			expectedStatus: http.StatusOK,
			expectResults:  true,
			description:    "Should find addresses with Oakley in street name",
		},
		{
			name:           "Search by city",
			city:           "Cincinnati",
			expectedStatus: http.StatusOK,
			expectResults:  true,
			description:    "Should find addresses in Cincinnati",
		},
		{
			name:           "Search by county",
			county:         "Hamilton",
			expectedStatus: http.StatusOK,
			expectResults:  true,
			description:    "Should find addresses in Hamilton County",
		},
		{
			name:           "Search by postcode",
			postcode:       "45209",
			expectedStatus: http.StatusOK,
			expectResults:  true,
			description:    "Should find addresses with ZIP code 45209",
		},
		{
			name:           "Combined search - city and street",
			city:           "Cincinnati",
			street:         "Oakley",
			expectedStatus: http.StatusOK,
			expectResults:  true,
			description:    "Should find Oakley addresses in Cincinnati",
		},
		{
			name:           "No results expected",
			query:          "XYZ123NonexistentAddress",
			expectedStatus: http.StatusOK,
			expectResults:  false,
			description:    "Should return empty results for non-existent address",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create Echo instance
			e := echo.New()

			// Build query string
			queryParams := ""
			if tt.query != "" {
				queryParams += fmt.Sprintf("query=%s&", tt.query)
			}
			if tt.county != "" {
				queryParams += fmt.Sprintf("county=%s&", tt.county)
			}
			if tt.city != "" {
				queryParams += fmt.Sprintf("city=%s&", tt.city)
			}
			if tt.street != "" {
				queryParams += fmt.Sprintf("street=%s&", tt.street)
			}
			if tt.postcode != "" {
				queryParams += fmt.Sprintf("postcode=%s&", tt.postcode)
			}

			// Create request
			req := httptest.NewRequest(http.MethodGet, "/api/v1/addresses?"+queryParams, nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			// Execute handler
			err := SearchOhioAddressesHandler(c)

			// Assertions
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedStatus, rec.Code)

			// Parse response
			var response models.AddressSearchResponse
			err = json.Unmarshal(rec.Body.Bytes(), &response)
			assert.NoError(t, err)

			// Check success
			assert.True(t, response.Success, tt.description)

			// Check if results are as expected
			if tt.expectResults {
				assert.Greater(t, response.Count, 0, "Expected to find results but got none: %s", tt.description)
				assert.Greater(t, len(response.Data), 0, "Expected data array to have items")

				// Verify data structure
				if len(response.Data) > 0 {
					addr := response.Data[0]
					assert.NotEmpty(t, addr.ID, "Address should have an ID")
					assert.NotEmpty(t, addr.County, "Address should have a county")

					// Log the first result for debugging
					t.Logf("Found address: %s %s, %s, %s %s",
						addr.HouseNumber, addr.Street, addr.City, addr.Region, addr.Postcode)
				}
			} else {
				assert.Equal(t, 0, response.Count, "Expected no results but got some: %s", tt.description)
			}

			// Log response for debugging
			t.Logf("%s: Found %d addresses (Total: %d)", tt.name, response.Count, response.Total)
		})
	}
}

func TestSearchWithProximity(t *testing.T) {
	setupTestEnvironment(t)

	e := echo.New()

	// Test proximity search (Cincinnati downtown area)
	lat := 39.1031
	lng := -84.5120
	radius := 5.0 // 5km radius

	queryString := fmt.Sprintf("lat=%f&lng=%f&radius=%f", lat, lng, radius)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/addresses?"+queryString, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := SearchOhioAddressesHandler(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var response models.AddressSearchResponse
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	assert.NoError(t, err)

	assert.True(t, response.Success)

	if response.Count > 0 {
		t.Logf("Proximity search found %d addresses within %fkm", response.Count, radius)
		// Verify addresses are within reasonable distance
		for i, addr := range response.Data {
			if i >= 5 {
				break // Only log first 5
			}
			t.Logf("  - %s %s, %s", addr.HouseNumber, addr.Street, addr.City)
		}
	}
}

func TestGetOhioAddressById(t *testing.T) {
	setupTestEnvironment(t)

	// First, search for an address to get a valid ID
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/addresses?county=Hamilton&limit=1", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := SearchOhioAddressesHandler(c)
	assert.NoError(t, err)

	var searchResponse models.AddressSearchResponse
	err = json.Unmarshal(rec.Body.Bytes(), &searchResponse)
	assert.NoError(t, err)

	if searchResponse.Count == 0 {
		t.Skip("No addresses found in Hamilton County to test with")
		return
	}

	addressID := searchResponse.Data[0].ID

	// Now test getting that specific address
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/addresses/%d", addressID), nil)
	rec = httptest.NewRecorder()
	c = e.NewContext(req, rec)
	c.SetPath("/api/v1/addresses/:id")
	c.SetParamNames("id")
	c.SetParamValues(fmt.Sprintf("%d", addressID))

	err = GetOhioAddressHandler(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var response models.AddressSearchResponse
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	assert.NoError(t, err)

	assert.True(t, response.Success)
	assert.Equal(t, 1, response.Count)
	assert.Equal(t, addressID, response.Data[0].ID)

	t.Logf("Retrieved address: %s %s, %s",
		response.Data[0].HouseNumber, response.Data[0].Street, response.Data[0].City)
}

func TestInvalidAddressId(t *testing.T) {
	setupTestEnvironment(t)

	e := echo.New()

	tests := []struct {
		name           string
		id             string
		expectedStatus int
	}{
		{
			name:           "Invalid ID format",
			id:             "abc",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "Non-existent ID",
			id:             "999999999",
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/addresses/%s", tt.id), nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetPath("/api/v1/addresses/:id")
			c.SetParamNames("id")
			c.SetParamValues(tt.id)

			err := GetOhioAddressHandler(c)
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedStatus, rec.Code)

			var response models.AddressSearchResponse
			err = json.Unmarshal(rec.Body.Bytes(), &response)
			assert.NoError(t, err)
			assert.False(t, response.Success)
		})
	}
}
