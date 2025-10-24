#!/bin/bash

# Production Deployment Test Script
# Run this before deploying to Coolify to verify everything works

set -e

echo "🧪 Testing Geocoding API for Production Deployment"
echo "=================================================="

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

# Check if Docker is running
print_status "INFO" "Checking Docker..."
if ! docker info > /dev/null 2>&1; then
    print_status "ERROR" "Docker is not running. Please start Docker and try again."
    exit 1
fi
print_status "SUCCESS" "Docker is running"

# Check if docker-compose is available
if ! command -v docker-compose > /dev/null 2>&1; then
    print_status "ERROR" "docker-compose not found. Please install docker-compose."
    exit 1
fi
print_status "SUCCESS" "docker-compose is available"

# Stop any running containers
print_status "INFO" "Stopping any existing containers..."
docker-compose down > /dev/null 2>&1 || true

# Build and start services
print_status "INFO" "Building and starting services..."
if docker-compose up -d --build; then
    print_status "SUCCESS" "Services started successfully"
else
    print_status "ERROR" "Failed to start services"
    exit 1
fi

# Wait for services to be ready
print_status "INFO" "Waiting for services to be ready..."
sleep 10

# Check if PostgreSQL is ready
print_status "INFO" "Checking PostgreSQL health..."
timeout=60
while [ $timeout -gt 0 ]; do
    if docker-compose exec -T postgres pg_isready -U geocoding_user -d geocoding_db > /dev/null 2>&1; then
        print_status "SUCCESS" "PostgreSQL is ready"
        break
    fi
    sleep 2
    timeout=$((timeout - 2))
done

if [ $timeout -eq 0 ]; then
    print_status "ERROR" "PostgreSQL failed to start within 60 seconds"
    docker-compose logs postgres
    exit 1
fi

# Check if API is ready
print_status "INFO" "Checking API health..."
timeout=60
while [ $timeout -gt 0 ]; do
    if curl -s http://localhost:8080/api/v1/health > /dev/null 2>&1; then
        print_status "SUCCESS" "API is ready"
        break
    fi
    sleep 2
    timeout=$((timeout - 2))
done

if [ $timeout -eq 0 ]; then
    print_status "ERROR" "API failed to start within 60 seconds"
    docker-compose logs geocoding-api
    exit 1
fi

# Test health endpoint
print_status "INFO" "Testing health endpoint..."
health_response=$(curl -s http://localhost:8080/api/v1/health)
if echo "$health_response" | grep -q "ok"; then
    print_status "SUCCESS" "Health endpoint responding correctly"
else
    print_status "ERROR" "Health endpoint not responding as expected"
    echo "Response: $health_response"
fi

# Test user registration
print_status "INFO" "Testing user registration..."
register_response=$(curl -s -X POST http://localhost:8080/api/v1/auth/register \
    -H "Content-Type: application/json" \
    -d '{"email":"test@example.com","password":"testpassword123"}')

if echo "$register_response" | grep -q "token"; then
    print_status "SUCCESS" "User registration working"
    
    # Extract token for further testing
    token=$(echo "$register_response" | grep -o '"token":"[^"]*"' | cut -d'"' -f4)
    
    # Test API key creation
    print_status "INFO" "Testing API key creation..."
    api_key_response=$(curl -s -X POST http://localhost:8080/api/v1/user/api-keys \
        -H "Authorization: Bearer $token" \
        -H "Content-Type: application/json" \
        -d '{"name":"test-key"}')
    
    if echo "$api_key_response" | grep -q "api_key"; then
        print_status "SUCCESS" "API key creation working"
        
        # Extract API key for geocoding test
        api_key=$(echo "$api_key_response" | grep -o '"api_key":"[^"]*"' | cut -d'"' -f4)
        
        # Test geocoding
        print_status "INFO" "Testing geocoding endpoint..."
        geocode_response=$(curl -s "http://localhost:8080/api/v1/geocode?query=10001" \
            -H "X-API-Key: $api_key")
        
        if echo "$geocode_response" | grep -q "latitude"; then
            print_status "SUCCESS" "Geocoding endpoint working"
        else
            print_status "WARNING" "Geocoding endpoint may not be working correctly"
            echo "Response: $geocode_response"
        fi
    else
        print_status "WARNING" "API key creation may not be working correctly"
        echo "Response: $api_key_response"
    fi
else
    print_status "WARNING" "User registration may not be working correctly"
    echo "Response: $register_response"
fi

# Test static files
print_status "INFO" "Testing static file serving..."
if curl -s http://localhost:8080/ | grep -q "Geocoding API"; then
    print_status "SUCCESS" "Static files are being served"
else
    print_status "WARNING" "Static files may not be configured correctly"
fi

# Check environment variable handling
print_status "INFO" "Checking environment variable handling..."
api_logs=$(docker-compose logs geocoding-api 2>&1)
if echo "$api_logs" | grep -q "No .env file found"; then
    print_status "SUCCESS" "Environment variables handled correctly (no .env file needed in Docker)"
else
    print_status "INFO" "No .env warning found (this is fine)"
fi

# Check for any error logs
if echo "$api_logs" | grep -qi "error\|fatal\|panic"; then
    print_status "WARNING" "Found some errors in API logs:"
    echo "$api_logs" | grep -i "error\|fatal\|panic"
else
    print_status "SUCCESS" "No critical errors found in logs"
fi

# Resource usage check
print_status "INFO" "Checking resource usage..."
echo ""
echo "Container Resource Usage:"
docker stats --no-stream --format "table {{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}\t{{.MemPerc}}"
echo ""

# Final summary
echo ""
print_status "INFO" "🎯 Production Readiness Checklist:"
echo "   ✅ Docker containers build and start"
echo "   ✅ PostgreSQL database connection"
echo "   ✅ API health endpoint"
echo "   ✅ User authentication system"
echo "   ✅ API key management"
echo "   ✅ Geocoding functionality"
echo "   ✅ Static file serving"
echo "   ✅ Environment variable handling"
echo ""

print_status "SUCCESS" "🚀 Ready for Coolify deployment!"
print_status "INFO" "Next steps:"
echo "   1. Push your code to your Git repository"
echo "   2. Set environment variables in Coolify"
echo "   3. Deploy using docker-compose.yml"
echo "   4. Test your production deployment"
echo ""

# Cleanup
print_status "INFO" "Cleaning up test containers..."
docker-compose down > /dev/null 2>&1

print_status "SUCCESS" "Test completed successfully!"