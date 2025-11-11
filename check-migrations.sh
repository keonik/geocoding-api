#!/bin/bash

# Migration Status Checker
# Run this script to check which migrations have been applied

set -e

echo "🔍 Checking Database Migration Status"
echo "====================================="

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

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

# Check if containers are running
if ! docker ps | grep -q "geocoding_db"; then
    print_status "ERROR" "Database container not running. Start with: docker-compose up -d"
    exit 1
fi

print_status "INFO" "Checking migration status..."

# Check if migrations table exists
print_status "INFO" "Checking if schema_migrations table exists..."
if docker exec geocoding_db psql -U geocoding_user -d geocoding_db -t -c "SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'schema_migrations');" | grep -q "t"; then
    print_status "SUCCESS" "Migrations table exists"
else
    print_status "WARNING" "Migrations table does not exist - migrations may not have run"
    exit 1
fi

# Show applied migrations
print_status "INFO" "Applied migrations:"
echo ""
docker exec geocoding_db psql -U geocoding_user -d geocoding_db -c "
SELECT 
    version,
    description,
    applied_at
FROM schema_migrations 
ORDER BY version;
"

echo ""
print_status "INFO" "Expected migrations:"
echo "  Version 1: Create zip_codes table"
echo "  Version 2: Create migration tracking table"
echo "  Version 3: Create authentication tables"
echo "  Version 4: Add name and company fields to users table"
echo "  Version 5: Add key_preview and expires_at to api_keys table"
echo "  Version 6: Update subscriptions table with billing columns"

echo ""

# Check for missing migrations
missing_migrations=$(docker exec geocoding_db psql -U geocoding_user -d geocoding_db -t -c "
WITH expected_migrations AS (
    SELECT unnest(ARRAY[1,2,3,4,5,6]) as version
)
SELECT string_agg(em.version::text, ', ')
FROM expected_migrations em
LEFT JOIN schema_migrations sm ON em.version = sm.version
WHERE sm.version IS NULL;
" | tr -d ' ')

if [ -n "$missing_migrations" ] && [ "$missing_migrations" != "" ]; then
    print_status "ERROR" "Missing migrations: $missing_migrations"
    print_status "INFO" "To fix: Restart the API container to run missing migrations"
    echo "  docker-compose restart geocoding-api"
else
    print_status "SUCCESS" "All expected migrations are applied!"
fi

# Check specific table structures that commonly cause issues
echo ""
print_status "INFO" "Checking critical table structures..."

# Check subscriptions table for new columns
print_status "INFO" "Checking subscriptions table structure..."
if docker exec geocoding_db psql -U geocoding_user -d geocoding_db -t -c "SELECT column_name FROM information_schema.columns WHERE table_name = 'subscriptions' AND column_name = 'price_per_call';" | grep -q "price_per_call"; then
    print_status "SUCCESS" "Subscriptions table has price_per_call column"
else
    print_status "ERROR" "Missing price_per_call column in subscriptions table"
    print_status "INFO" "This will cause billing errors. Run migration 6."
fi

if docker exec geocoding_db psql -U geocoding_user -d geocoding_db -t -c "SELECT column_name FROM information_schema.columns WHERE table_name = 'subscriptions' AND column_name = 'status';" | grep -q "status"; then
    print_status "SUCCESS" "Subscriptions table has status column"
else
    print_status "ERROR" "Missing status column in subscriptions table"
fi

# Check usage_records for created_at (not timestamp)
print_status "INFO" "Checking usage_records table structure..."
if docker exec geocoding_db psql -U geocoding_user -d geocoding_db -t -c "SELECT column_name FROM information_schema.columns WHERE table_name = 'usage_records' AND column_name = 'created_at';" | grep -q "created_at"; then
    print_status "SUCCESS" "Usage records table has created_at column"
else
    print_status "ERROR" "Missing created_at column in usage_records table"
fi

# Check API keys for new columns
print_status "INFO" "Checking api_keys table structure..."
if docker exec geocoding_db psql -U geocoding_user -d geocoding_db -t -c "SELECT column_name FROM information_schema.columns WHERE table_name = 'api_keys' AND column_name = 'key_preview';" | grep -q "key_preview"; then
    print_status "SUCCESS" "API keys table has key_preview column"
else
    print_status "ERROR" "Missing key_preview column in api_keys table"
fi

echo ""
print_status "SUCCESS" "Migration status check complete!"
print_status "INFO" "If you see any errors, restart the API container:"
echo "  docker-compose restart geocoding-api"
echo "  docker-compose logs geocoding-api | grep -i migration"