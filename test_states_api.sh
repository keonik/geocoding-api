#!/bin/bash

# Test script for US States API endpoints
# Run this after the server is started with the database

BASE_URL="http://localhost:8080/api/v1"
API_KEY="${API_KEY:-your-api-key-here}"

echo "🧪 Testing US States API Endpoints"
echo "=================================="
echo ""

# Color codes for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

test_count=0
pass_count=0
fail_count=0

# Function to test an endpoint
test_endpoint() {
    local name="$1"
    local url="$2"
    local expected_key="$3"
    
    test_count=$((test_count + 1))
    echo -e "${YELLOW}Test $test_count: $name${NC}"
    
    response=$(curl -s -H "X-API-Key: $API_KEY" "$url")
    
    if echo "$response" | jq -e ".$expected_key" > /dev/null 2>&1; then
        echo -e "${GREEN}✓ PASS${NC}"
        echo "$response" | jq -C ".$expected_key" | head -20
        pass_count=$((pass_count + 1))
    else
        echo -e "${RED}✗ FAIL${NC}"
        echo "$response" | jq -C . | head -20
        fail_count=$((fail_count + 1))
    fi
    echo ""
}

# Function to test coordinate lookup
test_coordinates() {
    local name="$1"
    local lat="$2"
    local lng="$3"
    local expected_state="$4"
    
    test_count=$((test_count + 1))
    echo -e "${YELLOW}Test $test_count: $name${NC}"
    echo "  Coordinates: ($lat, $lng)"
    
    response=$(curl -s -H "X-API-Key: $API_KEY" "$BASE_URL/states/lookup?lat=$lat&lng=$lng")
    state_abbr=$(echo "$response" | jq -r '.state.state_abbr')
    
    if [ "$state_abbr" = "$expected_state" ]; then
        echo -e "${GREEN}✓ PASS - Found $expected_state${NC}"
        echo "$response" | jq -C '.state | {state_name, state_abbr, state_fips}' 
        pass_count=$((pass_count + 1))
    else
        echo -e "${RED}✗ FAIL - Expected $expected_state, got $state_abbr${NC}"
        echo "$response" | jq -C . | head -10
        fail_count=$((fail_count + 1))
    fi
    echo ""
}

echo "📍 Testing State Search Endpoints"
echo "-----------------------------------"

# Test 1: Search all states
test_endpoint "Search all states" \
    "$BASE_URL/states?limit=5" \
    "states"

# Test 2: Search by name
test_endpoint "Search by name - California" \
    "$BASE_URL/states?name=california" \
    "states"

# Test 3: Search by abbreviation
test_endpoint "Search by abbreviation - TX" \
    "$BASE_URL/states?abbr=TX" \
    "states"

# Test 4: Search by partial name
test_endpoint "Search by partial name - new" \
    "$BASE_URL/states?name=new" \
    "states"

echo "🔍 Testing State Detail Endpoints"
echo "-----------------------------------"

# Test 5: Get state by abbreviation
test_endpoint "Get state by abbreviation - CA" \
    "$BASE_URL/states/CA" \
    "state"

# Test 6: Get state by FIPS code
test_endpoint "Get state by FIPS - 06 (California)" \
    "$BASE_URL/states/06" \
    "state"

# Test 7: Get state by name
test_endpoint "Get state by name - Texas" \
    "$BASE_URL/states/Texas" \
    "state"

echo "🗺️  Testing State Boundary Endpoints"
echo "--------------------------------------"

# Test 8: Get California boundary
test_count=$((test_count + 1))
echo -e "${YELLOW}Test $test_count: Get California boundary GeoJSON${NC}"
response=$(curl -s -H "X-API-Key: $API_KEY" "$BASE_URL/states/CA/boundary")
if echo "$response" | jq -e '.geometry.type' | grep -q "MultiPolygon"; then
    echo -e "${GREEN}✓ PASS - Got MultiPolygon geometry${NC}"
    echo "$response" | jq -C '{type, properties: .properties, geometry_type: .geometry.type}' 
    pass_count=$((pass_count + 1))
else
    echo -e "${RED}✗ FAIL - Invalid geometry${NC}"
    echo "$response" | jq -C . | head -10
    fail_count=$((fail_count + 1))
fi
echo ""

echo "📍 Testing Reverse Geocoding (Coordinates → State)"
echo "---------------------------------------------------"

# Test 9-14: Point-in-polygon lookups
test_coordinates "San Francisco, CA" "37.7749" "-122.4194" "CA"
test_coordinates "New York City, NY" "40.7128" "-74.0060" "NY"
test_coordinates "Austin, TX" "30.2672" "-97.7431" "TX"
test_coordinates "Miami, FL" "25.7617" "-80.1918" "FL"
test_coordinates "Seattle, WA" "47.6062" "-122.3321" "WA"
test_coordinates "Denver, CO" "39.7392" "-104.9903" "CO"

echo "❌ Testing Error Handling"
echo "-------------------------"

# Test invalid state
test_count=$((test_count + 1))
echo -e "${YELLOW}Test $test_count: Get non-existent state (ZZ)${NC}"
response=$(curl -s -w "\n%{http_code}" -H "X-API-Key: $API_KEY" "$BASE_URL/states/ZZ")
http_code=$(echo "$response" | tail -n1)
if [ "$http_code" = "404" ]; then
    echo -e "${GREEN}✓ PASS - Got 404 as expected${NC}"
    pass_count=$((pass_count + 1))
else
    echo -e "${RED}✗ FAIL - Expected 404, got $http_code${NC}"
    fail_count=$((fail_count + 1))
fi
echo ""

# Test coordinates in ocean
test_count=$((test_count + 1))
echo -e "${YELLOW}Test $test_count: Coordinates in ocean (no state)${NC}"
response=$(curl -s -w "\n%{http_code}" -H "X-API-Key: $API_KEY" "$BASE_URL/states/lookup?lat=35&lng=-130")
http_code=$(echo "$response" | tail -n1)
if [ "$http_code" = "404" ]; then
    echo -e "${GREEN}✓ PASS - Got 404 as expected${NC}"
    pass_count=$((pass_count + 1))
else
    echo -e "${RED}✗ FAIL - Expected 404, got $http_code${NC}"
    fail_count=$((fail_count + 1))
fi
echo ""

# Final summary
echo "=================================="
echo "📊 Test Summary"
echo "=================================="
echo -e "Total Tests:  $test_count"
echo -e "${GREEN}Passed:       $pass_count${NC}"
echo -e "${RED}Failed:       $fail_count${NC}"
echo ""

if [ $fail_count -eq 0 ]; then
    echo -e "${GREEN}✓ All tests passed!${NC}"
    exit 0
else
    echo -e "${RED}✗ Some tests failed${NC}"
    exit 1
fi
