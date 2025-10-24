#!/bin/bash

# Test API Usage Tracking
# This script tests API calls and checks if usage is being tracked correctly

set -e

echo "🔍 Testing API Usage Tracking"
echo "=============================="

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to print colored output
print_status() {
    local status=$1
    local message=$2
    case $status in
        "INFO")
            echo -e "${BLUE}ℹ️  $message${NC}"
            ;;
        "SUCCESS")
            echo -e "${GREEN}✅ $message${NC}"
            ;;
        "WARNING")
            echo -e "${YELLOW}⚠️  $message${NC}"
            ;;
        "ERROR")
            echo -e "${RED}❌ $message${NC}"
            ;;
    esac
}

# Check if API is running
API_URL="http://localhost:8080"
print_status "INFO" "Checking if API is running at $API_URL..."
if ! curl -s $API_URL/api/v1/health > /dev/null 2>&1; then
    print_status "ERROR" "API is not running. Please start with: docker-compose up"
    exit 1
fi
print_status "SUCCESS" "API is running"

# Get API key from user
echo ""
print_status "INFO" "Please provide an API key to test with:"
echo "  1. Go to http://localhost:8080"
echo "  2. Login/register"
echo "  3. Create an API key"
echo "  4. Copy the key here"
echo ""
read -p "Enter your API key: " API_KEY

if [ -z "$API_KEY" ]; then
    print_status "ERROR" "API key is required"
    exit 1
fi

print_status "INFO" "Testing API calls with key: ${API_KEY:0:8}..."

# Make several test API calls
echo ""
print_status "INFO" "Making test API calls..."

# Test 1: Geocode lookup
print_status "INFO" "Test 1: Geocoding NYC (10001)"
response1=$(curl -s -w "%{http_code}" \
    -H "X-API-Key: $API_KEY" \
    "$API_URL/api/v1/geocode/10001")
echo "Response: $response1"

# Test 2: Search
print_status "INFO" "Test 2: Search for Denver"
response2=$(curl -s -w "%{http_code}" \
    -H "X-API-Key: $API_KEY" \
    "$API_URL/api/v1/search?city=Denver&state=CO")
echo "Response: $response2"

# Test 3: Distance calculation
print_status "INFO" "Test 3: Distance between 10001 and 90210"
response3=$(curl -s -w "%{http_code}" \
    -H "X-API-Key: $API_KEY" \
    "$API_URL/api/v1/distance/10001/90210")
echo "Response: $response3"

# Check database for usage records
echo ""
print_status "INFO" "Checking database for usage records..."

# Query the database directly
print_status "INFO" "Latest 5 usage records from database:"
docker exec geocoding_db psql -U geocoding_user -d geocoding_db -c "
SELECT 
    id, user_id, api_key_id, endpoint, method, status_code, 
    billable, created_at 
FROM usage_records 
ORDER BY created_at DESC 
LIMIT 5;
" || print_status "WARNING" "Could not query database directly"

# Check current usage via API
echo ""
print_status "INFO" "Getting current usage summary..."

# Get user info first
current_month=$(date +"%Y-%m")
print_status "INFO" "Checking usage for month: $current_month"

# Note: This would require authentication to work properly
print_status "INFO" "To check usage via web interface:"
echo "  1. Go to http://localhost:8080"
echo "  2. Login to your account"
echo "  3. Check the dashboard for current usage"

echo ""
print_status "SUCCESS" "Testing completed!"
print_status "INFO" "Check the API logs for usage recording messages:"
echo "  docker-compose logs geocoding-api | grep -i usage"