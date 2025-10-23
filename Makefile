.PHONY: dev build run test clean docker-up docker-down load-data

# Development with hot reload
dev:
	air

# Build the application
build:
	go build -o geocoding-api .

# Run the application normally
run: build
	./geocoding-api

# Run tests
test:
	go test ./...

# Clean build artifacts
clean:
	rm -f geocoding-api
	rm -rf tmp/

# Start PostgreSQL with Docker
docker-up:
	docker-compose up -d postgres

# Stop all Docker services
docker-down:
	docker-compose down

# Load ZIP code data (requires API to be running)
load-data:
	curl -X POST http://localhost:8080/api/v1/admin/load-data

# Full Docker setup
docker-full:
	docker-compose up -d

# Install dependencies
deps:
	go mod download
	go mod tidy

# Install development tools
install-tools:
	go install github.com/air-verse/air@latest
	go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

# Serve documentation locally (alternative to running full API)
docs:
	@echo "📚 Serving API documentation at http://localhost:3000"
	@echo "Press Ctrl+C to stop"
	@cd docs && python3 -m http.server 3000 2>/dev/null || python -m SimpleHTTPServer 3000

# Validate OpenAPI spec
validate-spec:
	@command -v swagger >/dev/null 2>&1 || { echo "Installing swagger-codegen..."; npm install -g swagger-codegen-cli; }
	swagger-codegen validate -i api-docs.yaml

# Debug documentation issues
debug-docs:
	@echo "🔍 Debugging documentation setup..."
	@echo "📁 Checking files:"
	@ls -la api-docs.yaml docs/ 2>/dev/null || echo "❌ Missing files"
	@echo "\n🌐 Testing API spec accessibility:"
	@curl -s http://localhost:8080/api-docs-test 2>/dev/null || echo "❌ API not running - start with 'make dev'"
	@echo "\n📄 Testing YAML spec:"
	@curl -s -I http://localhost:8080/api-docs.yaml 2>/dev/null || echo "❌ Spec not accessible"
	@echo "\n✅ Try visiting: http://localhost:8080/docs"

# Test distance endpoints (requires API to be running)
test-distance:
	@echo "🧪 Testing distance calculation endpoints..."
	@echo "\n📏 Distance between NYC and LA:"
	@curl -s "http://localhost:8080/api/v1/distance/10001/90210" | jq '.data // .' 2>/dev/null || echo "❌ API not running"
	@echo "\n🎯 ZIP codes near NYC (1 mile):"
	@curl -s "http://localhost:8080/api/v1/nearby/10001?radius=1&limit=5" | jq '.data[0:2] // .' 2>/dev/null || echo "❌ API not running"
	@echo "\n🔍 Proximity check (NYC area):"
	@curl -s "http://localhost:8080/api/v1/proximity/10001/10002?radius=1" | jq '.data // .' 2>/dev/null || echo "❌ API not running"

# Test all endpoints
test-all:
	@echo "🧪 Testing all API endpoints..."
	@echo "\n❤️ Health check:"
	@curl -s http://localhost:8080/api/v1/health | jq '.' 2>/dev/null || echo "❌ API not running"
	@echo "\n🏠 Geocode lookup:"
	@curl -s http://localhost:8080/api/v1/geocode/10001 | jq '.data.city_name // .' 2>/dev/null || echo "❌ No data"
	@echo "\n🔍 City search:"
	@curl -s "http://localhost:8080/api/v1/search?city=New York&state=NY&limit=3" | jq '.count // .' 2>/dev/null || echo "❌ No data"
	@make test-distance

# Database migration commands (using golang-migrate)
migrate-up:
	migrate -path migrations -database "postgres://postgres:postgres@localhost:8954/geocoding_db?sslmode=disable" up

migrate-down:
	migrate -path migrations -database "postgres://postgres:postgres@localhost:8954/geocoding_db?sslmode=disable" down

migrate-create:
	@read -p "Enter migration name: " name; \
	migrate create -ext sql -dir migrations $$name

# Check migration status
migrate-version:
	migrate -path migrations -database "postgres://postgres:postgres@localhost:8954/geocoding_db?sslmode=disable" version