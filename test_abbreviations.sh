#!/bin/bash
# Test script for street abbreviation expansion

set -e

echo "🧪 Testing Street Abbreviation Expansion"
echo "========================================"
echo ""

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Check if server is running
if ! curl -s http://localhost:8080/api/v1/health > /dev/null; then
    echo "❌ Server is not running. Please start the server first."
    exit 1
fi

echo "✅ Server is running"
echo ""

# Get API key from environment or use test key
API_KEY="${TEST_API_KEY:-your-test-key-here}"

echo "📝 Test Cases:"
echo "-------------"

# Test 1: Search with abbreviation "dr" should find "Drive"
echo ""
echo "${BLUE}Test 1: Search '7 westerfield dr'${NC}"
RESPONSE=$(curl -s -H "X-API-Key: $API_KEY" \
    "http://localhost:8080/api/v1/addresses/search?q=7%20westerfield%20dr&limit=3")
echo "$RESPONSE" | python3 -m json.tool | grep -A 2 "full_address" | head -6
echo ""

# Test 2: Search with abbreviation "st" should find "Street"  
echo "${BLUE}Test 2: Search '811 elwood st'${NC}"
RESPONSE=$(curl -s -H "X-API-Key: $API_KEY" \
    "http://localhost:8080/api/v1/addresses/search?q=811%20elwood%20st&limit=3")
echo "$RESPONSE" | python3 -m json.tool | grep -A 2 "full_address" | head -6
echo ""

# Test 3: Search with abbreviation "rd" should find "Road"
echo "${BLUE}Test 3: Search 'shroyer rd'${NC}"
RESPONSE=$(curl -s -H "X-API-Key: $API_KEY" \
    "http://localhost:8080/api/v1/addresses/search?q=shroyer%20rd&limit=3")
echo "$RESPONSE" | python3 -m json.tool | grep -A 2 "full_address" | head -6
echo ""

# Test 4: Search with abbreviation "ln" should find "Lane"
echo "${BLUE}Test 4: Search 'park ln'${NC}"
RESPONSE=$(curl -s -H "X-API-Key: $API_KEY" \
    "http://localhost:8080/api/v1/addresses/search?q=park%20ln&limit=3")
echo "$RESPONSE" | python3 -m json.tool | grep -A 2 "full_address" | head -6
echo ""

# Test 5: Search with full word "drive" should also work
echo "${BLUE}Test 5: Search 'westerfield drive'${NC}"
RESPONSE=$(curl -s -H "X-API-Key: $API_KEY" \
    "http://localhost:8080/api/v1/addresses/search?q=westerfield%20drive&limit=3")
echo "$RESPONSE" | python3 -m json.tool | grep -A 2 "full_address" | head -6
echo ""

# Test 6: Check database directly for expansion
echo "${BLUE}Test 6: Database check - Sample full_address values${NC}"
docker exec -i geocoding_db psql -U postgres -d geocoding_db -t -c \
    "SELECT full_address FROM ohio_addresses WHERE street LIKE '%Dr' LIMIT 5;" \
    | sed 's/^[ \t]*//' | grep -v '^$'
echo ""

echo "${GREEN}✅ All tests completed!${NC}"
echo ""
echo "Expected behavior:"
echo "  - Searching 'dr' should match 'Drive' in full_address"
echo "  - Searching 'st' should match 'Street' in full_address"  
echo "  - Searching 'rd' should match 'Road' in full_address"
echo "  - Full_address column should show expanded forms (Drive, not Dr)"
