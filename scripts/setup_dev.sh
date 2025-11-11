#!/bin/bash

# Setup script for local development with GDAL

echo "🚀 Setting up Geocoding API development environment..."
echo ""

# Detect OS
OS="unknown"
if [[ "$OSTYPE" == "darwin"* ]]; then
    OS="macOS"
elif [[ "$OSTYPE" == "linux-gnu"* ]]; then
    OS="Linux"
elif [[ "$OSTYPE" == "msys" || "$OSTYPE" == "cygwin" ]]; then
    OS="Windows"
fi

echo "Detected OS: $OS"
echo ""

# Check Go installation
echo "Checking Go installation..."
if ! command -v go &> /dev/null; then
    echo "❌ Go is not installed!"
    echo "Please install Go 1.21 or later from: https://golang.org/dl/"
    exit 1
fi

GO_VERSION=$(go version | awk '{print $3}')
echo "✅ Go is installed: $GO_VERSION"
echo ""

# Check GDAL installation
echo "Checking GDAL installation..."
if ! command -v ogr2ogr &> /dev/null; then
    echo "❌ GDAL (ogr2ogr) is not installed!"
    echo ""
    echo "To install GDAL:"
    
    if [ "$OS" == "macOS" ]; then
        echo "  Run: brew install gdal"
    elif [ "$OS" == "Linux" ]; then
        if command -v apt-get &> /dev/null; then
            echo "  Run: sudo apt-get install gdal-bin"
        elif command -v yum &> /dev/null; then
            echo "  Run: sudo yum install gdal"
        fi
    elif [ "$OS" == "Windows" ]; then
        echo "  Download from: https://gdal.org/download.html"
    fi
    
    echo ""
    echo "⚠️  You can continue without GDAL, but shapefile conversion will not work."
    read -p "Continue anyway? (y/N): " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        exit 1
    fi
else
    GDAL_VERSION=$(ogr2ogr --version)
    echo "✅ GDAL is installed: $GDAL_VERSION"
fi
echo ""

# Check Docker
echo "Checking Docker installation..."
if ! command -v docker &> /dev/null; then
    echo "❌ Docker is not installed!"
    echo "Please install Docker from: https://www.docker.com/get-started"
    exit 1
fi

DOCKER_VERSION=$(docker --version)
echo "✅ Docker is installed: $DOCKER_VERSION"
echo ""

# Check Docker Compose
echo "Checking Docker Compose installation..."
if ! command -v docker-compose &> /dev/null && ! docker compose version &> /dev/null 2>&1; then
    echo "❌ Docker Compose is not installed!"
    echo "Please install Docker Compose from: https://docs.docker.com/compose/install/"
    exit 1
fi

echo "✅ Docker Compose is installed"
echo ""

# Install Go dependencies
echo "Installing Go dependencies..."
go mod download
if [ $? -eq 0 ]; then
    echo "✅ Go dependencies installed"
else
    echo "❌ Failed to install Go dependencies"
    exit 1
fi
echo ""

# Create necessary directories
echo "Creating necessary directories..."
mkdir -p oh cache test_conversion
echo "✅ Directories created"
echo ""

# Start Docker services
echo "Starting Docker services (PostgreSQL + pgAdmin)..."
docker compose up -d db pgadmin
if [ $? -eq 0 ]; then
    echo "✅ Docker services started"
    echo ""
    echo "Services available at:"
    echo "  - PostgreSQL: localhost:8954"
    echo "  - pgAdmin: http://localhost:5050"
    echo "    Email: admin@admin.com"
    echo "    Password: admin"
else
    echo "❌ Failed to start Docker services"
    exit 1
fi
echo ""

# Wait for PostgreSQL to be ready
echo "Waiting for PostgreSQL to be ready..."
sleep 5

# Check database connection
echo "Testing database connection..."
until docker compose exec -T db pg_isready -U postgres &> /dev/null; do
    echo "Waiting for database..."
    sleep 2
done
echo "✅ Database is ready"
echo ""

# Optional: Run GDAL test
if command -v ogr2ogr &> /dev/null; then
    echo "Would you like to test GDAL shapefile conversion? (downloads ~1MB)"
    read -p "Run GDAL test? (y/N): " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        ./scripts/test_gdal.sh
    fi
    echo ""
fi

echo "✅ Setup complete!"
echo ""
echo "Next steps:"
echo "  1. Start the development server:"
echo "     go run main.go"
echo ""
echo "  2. Or use air for hot reloading:"
echo "     air"
echo ""
echo "  3. Access the API at:"
echo "     http://localhost:8080"
echo ""
echo "  4. View API documentation:"
echo "     http://localhost:8080/docs"
echo ""
echo "  5. To download and convert Ohio address data:"
echo "     The data will be automatically downloaded on first run"
echo "     or you can manually trigger it during migration"
echo ""
