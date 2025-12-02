#!/bin/bash

# Test bulk upload endpoint
# Usage: ./test_bulk_upload.sh <auth_token> [base_url]

TOKEN="${1:-}"
BASE_URL="${2:-http://localhost:8080}"

if [ -z "$TOKEN" ]; then
    echo "Usage: ./test_bulk_upload.sh <auth_token> [base_url]"
    echo ""
    echo "Get your token from browser localStorage:"
    echo "  localStorage.getItem('authToken')"
    exit 1
fi

echo "Testing bulk upload endpoint..."
echo "Base URL: $BASE_URL"
echo ""

# First, test if the endpoint is reachable with a simple OPTIONS request
echo "1. Testing OPTIONS (CORS preflight)..."
curl -s -X OPTIONS "$BASE_URL/api/v1/admin/datasets/upload-bulk" \
    -H "Origin: http://localhost:3000" \
    -H "Access-Control-Request-Method: POST" \
    -H "Access-Control-Request-Headers: Authorization, Content-Type" \
    -w "\nHTTP Status: %{http_code}\n" \
    -o /dev/null

echo ""
echo "2. Testing POST without file (should get validation error)..."
curl -s -X POST "$BASE_URL/api/v1/admin/datasets/upload-bulk" \
    -H "Authorization: Bearer $TOKEN" \
    -F "state=OH" \
    | jq . 2>/dev/null || cat

echo ""
echo "3. Testing with a dummy file..."
# Create a small test file
echo '{"type":"FeatureCollection","features":[]}' > /tmp/test-upload.geojson

curl -s -X POST "$BASE_URL/api/v1/admin/datasets/upload-bulk" \
    -H "Authorization: Bearer $TOKEN" \
    -F "state=OH" \
    -F "files=@/tmp/test-upload.geojson" \
    | jq . 2>/dev/null || cat

rm /tmp/test-upload.geojson

echo ""
echo "Done!"
