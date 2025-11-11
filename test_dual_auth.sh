#!/bin/bash

# Test script to demonstrate dual authentication support
# Shows both X-API-Key and Authorization Bearer methods work

echo "🔐 Testing Dual Authentication Support"
echo "====================================="

# Test endpoint that requires authentication
ENDPOINT="http://localhost:8080/api/v1/health"

echo "Testing health endpoint (no auth required):"
echo "curl -s \"$ENDPOINT\""
curl -s "$ENDPOINT" | jq .
echo

echo "Testing protected endpoint with different auth methods:"
echo "Note: Replace 'your-valid-api-key' with an actual API key to test"
echo

PROTECTED_ENDPOINT="http://localhost:8080/api/v1/counties?limit=1"

echo "Method 1 - X-API-Key Header:"
echo "curl -H \"X-API-Key: your-valid-api-key\" \"$PROTECTED_ENDPOINT\""
echo

echo "Method 2 - Authorization Bearer:"  
echo "curl -H \"Authorization: Bearer your-valid-api-key\" \"$PROTECTED_ENDPOINT\""
echo

echo "Expected behavior:"
echo "- Both methods should work identically"
echo "- Invalid keys return 401 with helpful error message"
echo "- Missing auth returns 401 with both header options mentioned"
echo

echo "Testing missing authentication:"
curl -s "$PROTECTED_ENDPOINT" | jq .
echo

echo "Testing invalid Authorization format:"
curl -s -H "Authorization: InvalidFormat some-key" "$PROTECTED_ENDPOINT" | jq .
echo

echo "Testing invalid API key (X-API-Key):"
curl -s -H "X-API-Key: invalid-key" "$PROTECTED_ENDPOINT" | jq .
echo

echo "Testing invalid API key (Authorization Bearer):"
curl -s -H "Authorization: Bearer invalid-key" "$PROTECTED_ENDPOINT" | jq .