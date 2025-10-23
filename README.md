# Geocoding API

A Go-based REST API for geocoding location lookup using US ZIP codes, built with Echo framework and PostgreSQL database.

## Features

- 🏃 Fast geocoding lookup by ZIP code
- 🔍 Search ZIP codes by city name and state
- 🗄️ PostgreSQL database with full US ZIP code dataset
- 🚀 Built with Echo web framework
- 🐳 Docker support with Docker Compose
- 📊 Comprehensive location data including population, density, coordinates, and county information

## 📚 API Documentation

### **Interactive Documentation (Scalar)**
- **Local**: http://localhost:8080/docs 
- **Advanced**: http://localhost:8080/docs/advanced.html
- **Fallback**: http://localhost:8080/docs/fallback.html

### **OpenAPI Specification Endpoints**
- **Primary YAML**: http://localhost:8080/api-docs.yaml
- **Alternative URLs**: 
  - http://localhost:8080/openapi.yaml
  - http://localhost:8080/swagger.yaml  
  - http://localhost:8080/spec
- **JSON Info**: http://localhost:8080/api-docs.json (redirects to YAML)
- **Discovery**: http://localhost:8080/api-docs-test

### **Documentation Features**
- 🚀 **Interactive API explorer** - Test endpoints directly in browser
- 📖 **Comprehensive examples** - Real ZIP code data samples  
- 🎨 **Beautiful Scalar UI** - Modern, responsive design
- 🔍 **Searchable documentation** - Quick navigation with hotkeys
- 💾 **Multiple code samples** - JavaScript, cURL, and more
- 🌐 **Multi-environment support** - Switch between dev/prod servers

### **Quick Start with Docs**
```bash
# Start API with docs
make dev

# Visit documentation
open http://localhost:8080/docs

# Or serve docs only
make docs
```

## API Endpoints

### Get ZIP Code Information
```
GET /api/v1/geocode/{zipcode}
```

Returns detailed information for a specific ZIP code.

**Example:**
```bash
curl http://localhost:8080/api/v1/geocode/10001
```

**Response:**
```json
{
  "success": true,
  "data": {
    "zip_code": "10001",
    "city_name": "New York",
    "state_code": "NY",
    "state_name": "New York",
    "zcta": true,
    "population": 21102.0,
    "density": 35400.5,
    "primary_county_code": "36061",
    "primary_county_name": "New York",
    "county_weights": {"36061": "100"},
    "county_names": ["New York"],
    "county_codes": ["36061"],
    "imprecise": false,
    "military": false,
    "timezone": "America/New_York",
    "latitude": 40.75066,
    "longitude": -73.99670
  },
  "count": 1
}
```

### Search ZIP Codes by City
```
GET /api/v1/search?city={city_name}&state={state_code}&limit={limit}
```

Search for ZIP codes by city name, optionally filtered by state.

**Parameters:**
- `city` (required): City name to search for
- `state` (optional): Two-letter state code (e.g., "NY", "CA")
- `limit` (optional): Maximum number of results (default: 50, max: 100)

**Example:**
```bash
curl "http://localhost:8080/api/v1/search?city=Springfield&state=IL&limit=10"
```

### Calculate Distance Between ZIP Codes
```
GET /api/v1/distance/{from}/{to}
```

Calculate the precise distance between two ZIP codes using the Haversine formula.

**Example:**
```bash
curl http://localhost:8080/api/v1/distance/10001/90210
```

**Response:**
```json
{
  "success": true,
  "data": {
    "from_zip_code": "10001",
    "to_zip_code": "90210", 
    "distance_miles": 2445.5,
    "distance_km": 3936.2
  },
  "count": 1
}
```

### Find Nearby ZIP Codes
```
GET /api/v1/nearby/{zipcode}?radius={miles}&limit={limit}
```

Find all ZIP codes within a specified radius of a center ZIP code.

**Parameters:**
- `radius` (optional): Search radius in miles (default: 1, max: 100)
- `limit` (optional): Maximum results (default: 50, max: 200)

**Example:**
```bash
curl "http://localhost:8080/api/v1/nearby/10001?radius=5&limit=10"
```

### Check ZIP Code Proximity
```
GET /api/v1/proximity/{center}/{target}?radius={miles}
```

Check if a target ZIP code is within a specified radius of a center ZIP code.

**Example:**
```bash
curl "http://localhost:8080/api/v1/proximity/10001/10002?radius=1"
```

**Response:**
```json
{
  "success": true,
  "data": {
    "center_zip_code": "10001",
    "target_zip_code": "10002",
    "radius_miles": 1,
    "is_within_radius": true,
    "actual_distance_miles": 0.5,
    "actual_distance_km": 0.8
  },
  "count": 1
}
```

### Health Check
```
GET /api/v1/health
```

Returns the API health status.

### Load Data (Admin)
```
POST /api/v1/admin/load-data?file={csv_file_path}
```

Loads ZIP code data from CSV file into the database.

## Quick Start

### Using Docker Compose (Recommended)

1. **Clone or set up the project:**
   ```bash
   git clone <your-repo-url>
   cd geocoding-api
   ```

2. **Ensure the CSV file is present:**
   Make sure `georef-united-states-of-america-zc-point.csv` is in the project root.

3. **Start the services:**
   ```bash
   docker-compose up -d
   ```

4. **Load the ZIP code data:**
   ```bash
   curl -X POST http://localhost:8080/api/v1/admin/load-data
   ```

5. **Test the API:**
   ```bash
   curl http://localhost:8080/api/v1/geocode/10001
   ```

### Manual Setup

#### Prerequisites
- Go 1.21 or later
- PostgreSQL 12 or later

#### Setup Steps

1. **Install dependencies:**
   ```bash
   go mod download
   ```

2. **Set up PostgreSQL database:**
   ```sql
   CREATE DATABASE geocoding_db;
   ```

3. **Configure environment variables:**
   ```bash
   cp .env.example .env
   # Edit .env with your database configuration
   ```

4. **Run the application:**
   ```bash
   go run main.go
   ```

5. **Load the ZIP code data:**
   ```bash
   curl -X POST http://localhost:8080/api/v1/admin/load-data
   ```

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `DB_HOST` | PostgreSQL host | `localhost` |
| `DB_PORT` | PostgreSQL port | `5432` |
| `DB_USER` | PostgreSQL username | `postgres` |
| `DB_PASSWORD` | PostgreSQL password | `postgres` |
| `DB_NAME` | PostgreSQL database name | `geocoding_db` |
| `DB_SSLMODE` | PostgreSQL SSL mode | `disable` |
| `PORT` | API server port | `8080` |

## Data Schema

The ZIP code data includes the following fields:

- **ZIP Code**: Primary identifier
- **City and State**: Official USPS names and codes
- **Population and Density**: Demographics data
- **Coordinates**: Latitude and longitude
- **County Information**: Primary county and weights for multi-county ZIP codes
- **Timezone**: Time zone identifier
- **Flags**: ZCTA, imprecise, military status

## Database Structure

```sql
CREATE TABLE zip_codes (
    zip_code VARCHAR(10) PRIMARY KEY,
    city_name VARCHAR(255) NOT NULL,
    state_code VARCHAR(2) NOT NULL,
    state_name VARCHAR(255) NOT NULL,
    zcta BOOLEAN NOT NULL DEFAULT FALSE,
    zcta_parent VARCHAR(10),
    population DECIMAL(12,2),
    density DECIMAL(10,2),
    primary_county_code VARCHAR(10) NOT NULL,
    primary_county_name VARCHAR(255) NOT NULL,
    county_weights JSONB,
    county_names TEXT,
    county_codes TEXT,
    imprecise BOOLEAN NOT NULL DEFAULT FALSE,
    military BOOLEAN NOT NULL DEFAULT FALSE,
    timezone VARCHAR(100) NOT NULL,
    latitude DECIMAL(10,7) NOT NULL,
    longitude DECIMAL(10,7) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

## Database Migrations

This project uses a custom migration system with version tracking. Migrations run automatically when the application starts.

### **Current Migration System (Built-in)**

The application automatically:
1. Creates a `schema_migrations` table to track applied migrations
2. Runs pending migrations in order
3. Loads ZIP code data from CSV if the database is empty

**Migration files are located in:**
- `database/migrations.go` - Migration definitions
- `services/zipcode_service.go` - Data loading logic

### **Alternative: golang-migrate (Optional)**

For more advanced migration needs, you can use `golang-migrate`:

```bash
# Install migrate tool
make install-tools

# Create a new migration
make migrate-create

# Apply migrations
make migrate-up

# Rollback migrations  
make migrate-down

# Check migration status
make migrate-version
```

**Migration files:** `migrations/*.sql`

### **Data Loading**

The application automatically loads ZIP code data on first run:

1. **Automatic**: Data loads when database is empty
2. **Manual**: `curl -X POST http://localhost:8080/api/v1/admin/load-data`
3. **Makefile**: `make load-data`

## Performance

The database includes several indexes for optimal query performance:

- Primary key index on `zip_code`
- Index on `state_code` for state-based filtering
- Index on `city_name` for city searches
- Composite index on `latitude, longitude` for geographical queries

## Error Handling

The API returns standardized error responses:

```json
{
  "success": false,
  "error": "Error description"
}
```

Common HTTP status codes:
- `200`: Success
- `400`: Bad Request (invalid parameters)
- `404`: Not Found (ZIP code not found)
- `500`: Internal Server Error

## Contributing

1. Fork the repository
2. Create a feature branch
3. Commit your changes
4. Push to the branch
5. Create a Pull Request

## License

This project is licensed under the MIT License - see the LICENSE file for details.

## Data Source

This API uses ZIP code data from the GeoNames geographical database. The data includes comprehensive information about US ZIP codes including geographical coordinates, administrative boundaries, and demographic information.