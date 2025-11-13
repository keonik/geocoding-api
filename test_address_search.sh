#!/bin/bash
# Integration tests for address search API

set -e

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Base URL
BASE_URL="http://localhost:8080"

# Test credentials
EMAIL="test-$(date +%s)@example.com"
PASSWORD="testpass123"

echo "=== Address Search Integration Tests ==="
echo

# Step 1: Register a test user
echo -n "1. Registering test user... "
REGISTER_RESPONSE=$(curl -s -X POST "$BASE_URL/api/v1/auth/register" \
  -H "Content-Type: application/json" \
  -d "{\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\",\"name\":\"Test User\"}")

TOKEN=$(echo $REGISTER_RESPONSE | python3 -c "import sys, json; data=json.load(sys.stdin); print(data['data']['token'])" 2>/dev/null || echo "")

if [ -z "$TOKEN" ]; then
  echo -e "${RED}FAILED${NC}"
  echo "Response: $REGISTER_RESPONSE"
  exit 1
fi
echo -e "${GREEN}OK${NC}"

# Step 2: Create API key
echo -n "2. Creating API key... "
API_KEY_RESPONSE=$(curl -s -X POST "$BASE_URL/api/v1/user/api-keys" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"test-key","permissions":["*"]}')

API_KEY=$(echo $API_KEY_RESPONSE | python3 -c "import sys, json; data=json.load(sys.stdin); print(data['data']['key_string'])" 2>/dev/null || echo "")

if [ -z "$API_KEY" ]; then
  echo -e "${RED}FAILED${NC}"
  echo "Response: $API_KEY_RESPONSE"
  exit 1
fi
echo -e "${GREEN}OK${NC}"

# Step 3: Test address search by query
echo -n "3. Testing search by query '2525 Oakley'... "
SEARCH_RESPONSE=$(curl -s "$BASE_URL/api/v1/addresses?query=2525%20Oakley" \
  -H "X-API-Key: $API_KEY")

COUNT=$(echo $SEARCH_RESPONSE | python3 -c "import sys, json; data=json.load(sys.stdin); print(data.get('count', 0))" 2>/dev/null || echo "0")

if [ "$COUNT" -gt 0 ]; then
  echo -e "${GREEN}OK - Found $COUNT addresses${NC}"
  echo "   Sample result:"
  echo $SEARCH_RESPONSE | python3 -c "
import sys, json
data = json.load(sys.stdin)
if data.get('data'):
    addr = data['data'][0]
    print(f\"   {addr.get('house_number')} {addr.get('street')}, {addr.get('city')} {addr.get('postcode')}\")
" 2>/dev/null || echo "   (Could not parse address)"
else
  echo -e "${RED}FAILED - No results found${NC}"
  echo "Response: $SEARCH_RESPONSE"
fi

# Step 4: Test search by street name
echo -n "4. Testing search by street 'Oakley'... "
SEARCH_RESPONSE=$(curl -s "$BASE_URL/api/v1/addresses?street=Oakley" \
  -H "X-API-Key: $API_KEY")

COUNT=$(echo $SEARCH_RESPONSE | python3 -c "import sys, json; data=json.load(sys.stdin); print(data.get('count', 0))" 2>/dev/null || echo "0")

if [ "$COUNT" -gt 0 ]; then
  echo -e "${GREEN}OK - Found $COUNT addresses${NC}"
else
  echo -e "${RED}FAILED - No results found${NC}"
  echo "Response: $SEARCH_RESPONSE"
fi

# Step 5: Test search by city
echo -n "5. Testing search by city 'Cincinnati'... "
SEARCH_RESPONSE=$(curl -s "$BASE_URL/api/v1/addresses?city=Cincinnati&limit=5" \
  -H "X-API-Key: $API_KEY")

COUNT=$(echo $SEARCH_RESPONSE | python3 -c "import sys, json; data=json.load(sys.stdin); print(data.get('count', 0))" 2>/dev/null || echo "0")

if [ "$COUNT" -gt 0 ]; then
  echo -e "${GREEN}OK - Found $COUNT addresses${NC}"
else
  echo -e "${RED}FAILED - No results found${NC}"
  echo "Response: $SEARCH_RESPONSE"
fi

# Step 6: Test search by county
echo -n "6. Testing search by county 'Hamilton'... "
SEARCH_RESPONSE=$(curl -s "$BASE_URL/api/v1/addresses?county=Hamilton&limit=5" \
  -H "X-API-Key: $API_KEY")

COUNT=$(echo $SEARCH_RESPONSE | python3 -c "import sys, json; data=json.load(sys.stdin); print(data.get('count', 0))" 2>/dev/null || echo "0")

if [ "$COUNT" -gt 0 ]; then
  echo -e "${GREEN}OK - Found $COUNT addresses${NC}"
else
  echo -e "${RED}FAILED - No results found${NC}"
  echo "Response: $SEARCH_RESPONSE"
fi

# Step 7: Test search by postcode
echo -n "7. Testing search by postcode '45209'... "
SEARCH_RESPONSE=$(curl -s "$BASE_URL/api/v1/addresses?postcode=45209&limit=5" \
  -H "X-API-Key: $API_KEY")

COUNT=$(echo $SEARCH_RESPONSE | python3 -c "import sys, json; data=json.load(sys.stdin); print(data.get('count', 0))" 2>/dev/null || echo "0")

if [ "$COUNT" -gt 0 ]; then
  echo -e "${GREEN}OK - Found $COUNT addresses${NC}"
else
  echo -e "${RED}FAILED - No results found${NC}"
  echo "Response: $SEARCH_RESPONSE"
fi

# Step 8: Test combined search
echo -n "8. Testing combined search (city=Cincinnati, street=Oakley)... "
SEARCH_RESPONSE=$(curl -s "$BASE_URL/api/v1/addresses?city=Cincinnati&street=Oakley&limit=5" \
  -H "X-API-Key: $API_KEY")

COUNT=$(echo $SEARCH_RESPONSE | python3 -c "import sys, json; data=json.load(sys.stdin); print(data.get('count', 0))" 2>/dev/null || echo "0")

if [ "$COUNT" -gt 0 ]; then
  echo -e "${GREEN}OK - Found $COUNT addresses${NC}"
else
  echo -e "${RED}FAILED - No results found${NC}"
  echo "Response: $SEARCH_RESPONSE"
fi

# Step 9: Test proximity search (Cincinnati downtown)
echo -n "9. Testing proximity search (39.1031, -84.5120, 2km radius)... "
SEARCH_RESPONSE=$(curl -s "$BASE_URL/api/v1/addresses?lat=39.1031&lng=-84.5120&radius=2&limit=5" \
  -H "X-API-Key: $API_KEY")

COUNT=$(echo $SEARCH_RESPONSE | python3 -c "import sys, json; data=json.load(sys.stdin); print(data.get('count', 0))" 2>/dev/null || echo "0")

if [ "$COUNT" -gt 0 ]; then
  echo -e "${GREEN}OK - Found $COUNT addresses${NC}"
else
  echo -e "${RED}FAILED - No results found${NC}"
  echo "Response: $SEARCH_RESPONSE"
fi

# Step 10: Test non-existent address
echo -n "10. Testing non-existent address... "
SEARCH_RESPONSE=$(curl -s "$BASE_URL/api/v1/addresses?query=XYZ999NonexistentStreet" \
  -H "X-API-Key: $API_KEY")

COUNT=$(echo $SEARCH_RESPONSE | python3 -c "import sys, json; data=json.load(sys.stdin); print(data.get('count', 0))" 2>/dev/null || echo "0")

if [ "$COUNT" -eq 0 ]; then
  echo -e "${GREEN}OK - Correctly returned 0 results${NC}"
else
  echo -e "${RED}FAILED - Expected 0 results, got $COUNT${NC}"
fi

echo
echo "=== Test Summary ==="
echo "All integration tests completed."
echo "If any tests failed, check the responses above for details."
