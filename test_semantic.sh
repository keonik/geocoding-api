#!/bin/bash

# Register user
USER_EMAIL="semantic-test-$(date +%s)@example.com"
echo "1. Registering user..."
USER_RESPONSE=$(curl -s -X POST http://localhost:8080/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d "{\"email\":\"$USER_EMAIL\",\"password\":\"password123\",\"name\":\"Test User\"}")

USER_ID=$(echo $USER_RESPONSE | python3 -c "import sys, json; data=json.load(sys.stdin); print(data['data']['id'])" 2>/dev/null)

if [ -z "$USER_ID" ]; then
  echo "Failed to register user"
  exit 1
fi
echo "OK - User ID: $USER_ID"

# Create API key
echo "2. Creating API key..."
API_KEY_RESPONSE=$(curl -s -X POST http://localhost:8080/api/v1/keys \
  -H "Content-Type: application/json" \
  -H "X-User-ID: $USER_ID" \
  -d '{"name":"test-key","permissions":["*"]}')

API_KEY=$(echo $API_KEY_RESPONSE | python3 -c "import sys, json; data=json.load(sys.stdin); print(data['data']['key_string'])" 2>/dev/null)

if [ -z "$API_KEY" ]; then
  echo "Failed to create API key"
  exit 1
fi
echo "OK - API Key created"

# Test semantic search
echo ""
echo "3. Testing semantic search for '2525 oakley'..."
RESPONSE=$(curl -s "http://localhost:8080/api/v1/addresses/semantic?q=2525%20oakley&limit=5" \
  -H "X-API-Key: $API_KEY")

echo "$RESPONSE" | python3 -c "
import sys, json
data = json.load(sys.stdin)
print(f\"Success: {data.get('success')}\")
print(f\"Count: {data.get('count', 0)}\")
if data.get('data'):
    print('First 3 results:')
    for addr in data['data'][0:3]:
        print(f\"  {addr.get('house_number')} {addr.get('street')}, {addr.get('city')}\")
else:
    print('No results')
    print('Full response:', json.dumps(data, indent=2))
"

echo ""
echo "4. Testing regular search for '2525 oakley'..."
RESPONSE2=$(curl -s "http://localhost:8080/api/v1/addresses?query=2525%20oakley&limit=5" \
  -H "X-API-Key: $API_KEY")

echo "$RESPONSE2" | python3 -c "
import sys, json
data = json.load(sys.stdin)
print(f\"Success: {data.get('success')}\")
print(f\"Count: {data.get('count', 0)}\")
if data.get('data'):
    print('First 3 results:')
    for addr in data['data'][0:3]:
        print(f\"  {addr.get('house_number')} {addr.get('street')}, {addr.get('city')}\")
"
