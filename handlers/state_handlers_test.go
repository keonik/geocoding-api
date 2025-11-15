package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"geocoding-api/database"
	"geocoding-api/models"
	"geocoding-api/services"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

// setupStateTestDB initializes the database for testing
func setupStateTestDB(t *testing.T) {
	if err := database.InitDB(); err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}

	// Ensure states table exists
	if err := database.RunMigrations(); err != nil {
		t.Fatalf("Failed to run migrations: %v", err)
	}

	// Load state data if not already loaded
	var count int
	err := database.DB.QueryRow("SELECT COUNT(*) FROM us_states").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to check states table: %v", err)
	}

	if count == 0 {
		if err := services.InitializeStateData(); err != nil {
			t.Logf("Warning: Failed to initialize state data: %v", err)
			t.Skip("Skipping test - state data not available")
		}
	}
}

func TestSearchStatesHandler(t *testing.T) {
	setupStateTestDB(t)

	tests := []struct {
		name           string
		queryParams    string
		expectedStatus int
		validateFunc   func(t *testing.T, response map[string]interface{})
	}{
		{
			name:           "Search all states",
			queryParams:    "",
			expectedStatus: http.StatusOK,
			validateFunc: func(t *testing.T, response map[string]interface{}) {
				states := response["states"].([]interface{})
				assert.Greater(t, len(states), 0, "Should return states")
				assert.Greater(t, int(response["total"].(float64)), 0, "Should have total count")
			},
		},
		{
			name:           "Search by name - California",
			queryParams:    "?name=california",
			expectedStatus: http.StatusOK,
			validateFunc: func(t *testing.T, response map[string]interface{}) {
				states := response["states"].([]interface{})
				assert.Greater(t, len(states), 0, "Should find California")
				state := states[0].(map[string]interface{})
				assert.Contains(t, state["state_name"].(string), "California")
			},
		},
		{
			name:           "Search by abbreviation - TX",
			queryParams:    "?abbr=TX",
			expectedStatus: http.StatusOK,
			validateFunc: func(t *testing.T, response map[string]interface{}) {
				states := response["states"].([]interface{})
				assert.Greater(t, len(states), 0, "Should find Texas")
				state := states[0].(map[string]interface{})
				assert.Equal(t, "TX", state["state_abbr"].(string))
			},
		},
		{
			name:           "Search by partial name",
			queryParams:    "?name=new",
			expectedStatus: http.StatusOK,
			validateFunc: func(t *testing.T, response map[string]interface{}) {
				states := response["states"].([]interface{})
				assert.Greater(t, len(states), 0, "Should find states with 'new' in name")
				// Should find New York, New Jersey, New Mexico, New Hampshire
			},
		},
		{
			name:           "Search with pagination",
			queryParams:    "?limit=5&offset=0",
			expectedStatus: http.StatusOK,
			validateFunc: func(t *testing.T, response map[string]interface{}) {
				states := response["states"].([]interface{})
				assert.LessOrEqual(t, len(states), 5, "Should respect limit")
				assert.Equal(t, float64(5), response["limit"].(float64))
				assert.Equal(t, float64(0), response["offset"].(float64))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/states"+tt.queryParams, nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err := SearchStatesHandler(c)
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedStatus, rec.Code)

			if tt.expectedStatus == http.StatusOK && tt.validateFunc != nil {
				var response map[string]interface{}
				err := json.Unmarshal(rec.Body.Bytes(), &response)
				assert.NoError(t, err)
				tt.validateFunc(t, response)
			}
		})
	}
}

func TestGetStateHandler(t *testing.T) {
	setupStateTestDB(t)

	tests := []struct {
		name           string
		identifier     string
		expectedStatus int
		validateFunc   func(t *testing.T, response map[string]interface{})
	}{
		{
			name:           "Get state by abbreviation - CA",
			identifier:     "CA",
			expectedStatus: http.StatusOK,
			validateFunc: func(t *testing.T, response map[string]interface{}) {
				state := response["state"].(map[string]interface{})
				assert.Equal(t, "CA", state["state_abbr"].(string))
				assert.Equal(t, "California", state["state_name"].(string))
				assert.NotEmpty(t, state["state_fips"].(string))
			},
		},
		{
			name:           "Get state by FIPS code - 06",
			identifier:     "06",
			expectedStatus: http.StatusOK,
			validateFunc: func(t *testing.T, response map[string]interface{}) {
				state := response["state"].(map[string]interface{})
				assert.Equal(t, "CA", state["state_abbr"].(string))
			},
		},
		{
			name:           "Get state by name - Texas",
			identifier:     "Texas",
			expectedStatus: http.StatusOK,
			validateFunc: func(t *testing.T, response map[string]interface{}) {
				state := response["state"].(map[string]interface{})
				assert.Equal(t, "TX", state["state_abbr"].(string))
				assert.Equal(t, "Texas", state["state_name"].(string))
			},
		},
		{
			name:           "Get state by name case insensitive",
			identifier:     "new york",
			expectedStatus: http.StatusOK,
			validateFunc: func(t *testing.T, response map[string]interface{}) {
				state := response["state"].(map[string]interface{})
				assert.Equal(t, "NY", state["state_abbr"].(string))
			},
		},
		{
			name:           "Get non-existent state",
			identifier:     "ZZ",
			expectedStatus: http.StatusNotFound,
			validateFunc:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/states/"+tt.identifier, nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("identifier")
			c.SetParamValues(tt.identifier)

			err := GetStateHandler(c)
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedStatus, rec.Code)

			if tt.expectedStatus == http.StatusOK && tt.validateFunc != nil {
				var response map[string]interface{}
				err := json.Unmarshal(rec.Body.Bytes(), &response)
				assert.NoError(t, err)
				tt.validateFunc(t, response)
			}
		})
	}
}

func TestGetStateBoundaryHandler(t *testing.T) {
	setupStateTestDB(t)

	tests := []struct {
		name           string
		identifier     string
		expectedStatus int
		validateFunc   func(t *testing.T, response map[string]interface{})
	}{
		{
			name:           "Get California boundary",
			identifier:     "CA",
			expectedStatus: http.StatusOK,
			validateFunc: func(t *testing.T, response map[string]interface{}) {
				assert.Equal(t, "Feature", response["type"].(string))
				
				properties := response["properties"].(map[string]interface{})
				assert.Equal(t, "CA", properties["state_abbr"].(string))
				assert.Equal(t, "California", properties["state_name"].(string))
				assert.Greater(t, properties["area_land"].(float64), float64(0))
				
				geometry := response["geometry"].(map[string]interface{})
				assert.Equal(t, "MultiPolygon", geometry["type"].(string))
				assert.NotNil(t, geometry["coordinates"])
			},
		},
		{
			name:           "Get Texas boundary by FIPS",
			identifier:     "48",
			expectedStatus: http.StatusOK,
			validateFunc: func(t *testing.T, response map[string]interface{}) {
				properties := response["properties"].(map[string]interface{})
				assert.Equal(t, "TX", properties["state_abbr"].(string))
			},
		},
		{
			name:           "Get boundary for non-existent state",
			identifier:     "ZZ",
			expectedStatus: http.StatusNotFound,
			validateFunc:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/states/"+tt.identifier+"/boundary", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("identifier")
			c.SetParamValues(tt.identifier)

			err := GetStateBoundaryHandler(c)
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedStatus, rec.Code)

			if tt.expectedStatus == http.StatusOK && tt.validateFunc != nil {
				var response map[string]interface{}
				err := json.Unmarshal(rec.Body.Bytes(), &response)
				assert.NoError(t, err)
				tt.validateFunc(t, response)
			}
		})
	}
}

func TestGetStateByLocationHandler(t *testing.T) {
	setupStateTestDB(t)

	tests := []struct {
		name           string
		lat            string
		lng            string
		expectedStatus int
		validateFunc   func(t *testing.T, response map[string]interface{})
	}{
		{
			name:           "Find state for San Francisco coordinates",
			lat:            "37.7749",
			lng:            "-122.4194",
			expectedStatus: http.StatusOK,
			validateFunc: func(t *testing.T, response map[string]interface{}) {
				state := response["state"].(map[string]interface{})
				assert.Equal(t, "CA", state["state_abbr"].(string))
				
				coords := response["coordinates"].(map[string]interface{})
				assert.Equal(t, 37.7749, coords["lat"].(float64))
				assert.Equal(t, -122.4194, coords["lng"].(float64))
			},
		},
		{
			name:           "Find state for New York City",
			lat:            "40.7128",
			lng:            "-74.0060",
			expectedStatus: http.StatusOK,
			validateFunc: func(t *testing.T, response map[string]interface{}) {
				state := response["state"].(map[string]interface{})
				assert.Equal(t, "NY", state["state_abbr"].(string))
			},
		},
		{
			name:           "Find state for Austin, TX",
			lat:            "30.2672",
			lng:            "-97.7431",
			expectedStatus: http.StatusOK,
			validateFunc: func(t *testing.T, response map[string]interface{}) {
				state := response["state"].(map[string]interface{})
				assert.Equal(t, "TX", state["state_abbr"].(string))
			},
		},
		{
			name:           "Coordinates in ocean - not in any state",
			lat:            "35.0",
			lng:            "-130.0",
			expectedStatus: http.StatusNotFound,
			validateFunc:   nil,
		},
		{
			name:           "Missing latitude parameter",
			lat:            "",
			lng:            "-122.4194",
			expectedStatus: http.StatusBadRequest,
			validateFunc:   nil,
		},
		{
			name:           "Missing longitude parameter",
			lat:            "37.7749",
			lng:            "",
			expectedStatus: http.StatusBadRequest,
			validateFunc:   nil,
		},
		{
			name:           "Invalid latitude value",
			lat:            "invalid",
			lng:            "-122.4194",
			expectedStatus: http.StatusBadRequest,
			validateFunc:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := echo.New()
			url := "/api/v1/states/lookup"
			if tt.lat != "" || tt.lng != "" {
				url += "?lat=" + tt.lat + "&lng=" + tt.lng
			}
			req := httptest.NewRequest(http.MethodGet, url, nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err := GetStateByLocationHandler(c)
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedStatus, rec.Code)

			if tt.expectedStatus == http.StatusOK && tt.validateFunc != nil {
				var response map[string]interface{}
				err := json.Unmarshal(rec.Body.Bytes(), &response)
				assert.NoError(t, err)
				tt.validateFunc(t, response)
			}
		})
	}
}

func TestStateServiceDirectly(t *testing.T) {
	setupStateTestDB(t)

	t.Run("Search states with filters", func(t *testing.T) {
		params := models.StateSearchParams{
			Name:  "California",
			Limit: 10,
		}
		
		response, err := services.State.SearchStates(params)
		assert.NoError(t, err)
		assert.NotNil(t, response)
		assert.Greater(t, len(response.States), 0)
		assert.Contains(t, response.States[0].StateName, "California")
	})

	t.Run("Get state by various identifiers", func(t *testing.T) {
		// By abbreviation
		state, err := services.State.GetStateByIdentifier("CA")
		assert.NoError(t, err)
		assert.Equal(t, "CA", state.StateAbbr)
		
		// By FIPS
		state, err = services.State.GetStateByIdentifier("06")
		assert.NoError(t, err)
		assert.Equal(t, "CA", state.StateAbbr)
		
		// By name
		state, err = services.State.GetStateByIdentifier("California")
		assert.NoError(t, err)
		assert.Equal(t, "CA", state.StateAbbr)
	})

	t.Run("Point-in-polygon lookup", func(t *testing.T) {
		// Los Angeles coordinates
		state, err := services.State.GetStateByCoordinates(34.0522, -118.2437)
		assert.NoError(t, err)
		assert.Equal(t, "CA", state.StateAbbr)
		
		// Miami coordinates
		state, err = services.State.GetStateByCoordinates(25.7617, -80.1918)
		assert.NoError(t, err)
		assert.Equal(t, "FL", state.StateAbbr)
	})

	t.Run("Get boundary GeoJSON", func(t *testing.T) {
		geoJSON, err := services.State.GetStateBoundaryGeoJSON("CA")
		assert.NoError(t, err)
		assert.NotNil(t, geoJSON)
		assert.Equal(t, "Feature", geoJSON["type"])
		
		properties := geoJSON["properties"].(map[string]interface{})
		assert.Equal(t, "CA", properties["state_abbr"])
		
		geometry := geoJSON["geometry"].(map[string]interface{})
		assert.Equal(t, "MultiPolygon", geometry["type"])
	})
}
