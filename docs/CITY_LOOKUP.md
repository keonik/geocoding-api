# City Lookup Integration

## Overview
US cities data has been integrated to provide a fallback mechanism when ZIP code searches fail. The system includes ~31,000 US cities with coordinates, population data, and associated ZIP codes.

## Data Source
- **File**: `uscities.csv.gz` (1.3MB compressed, 4.9MB uncompressed)
- **Records**: ~31,255 US cities
- **Fields**: city, state, county, coordinates, population, ZIP codes, and more

## Database Schema
The cities are stored in a new `cities` table with:
- Full-text search indexes (using pg_trgm)
- Spatial indexes for location-based queries
- Population and ranking for result ordering

## API Endpoints

### 1. Search Cities
```http
GET /api/v1/cities
```

**Query Parameters:**
- `query` - Free text search (city name)
- `city` - Filter by city name (fuzzy match)
- `state` - Filter by state ID (e.g., "OH") or state name
- `county` - Filter by county name
- `lat`, `lng`, `radius` - Search within radius (km) of coordinates
- `min_population` - Minimum population filter
- `limit` - Results per page (default: 10, max: 100)
- `offset` - Pagination offset

**Examples:**
```bash
# Search by city name
GET /api/v1/cities?city=Dayton&state=OH

# Free text search
GET /api/v1/cities?query=Dayton

# Search with population filter
GET /api/v1/cities?query=Dayton&min_population=100000

# Location-based search (within 50km)
GET /api/v1/cities?lat=39.7589&lng=-84.1916&radius=50
```

**Response:**
```json
{
  "success": true,
  "data": [
    {
      "id": 123,
      "city": "Dayton",
      "city_ascii": "Dayton",
      "state_id": "OH",
      "state_name": "Ohio",
      "county_name": "Montgomery",
      "lat": 39.7589,
      "lng": -84.1916,
      "population": 140407,
      "density": 1234.56,
      "timezone": "America/New_York",
      "ranking": 1,
      "zips": "45401 45402 45403 45404 45405 45406 45409 45410 ..."
    }
  ],
  "count": 1,
  "total": 1,
  "query": "Dayton",
  "filters": {
    "state": "OH"
  }
}
```

### 2. Get City by ID
```http
GET /api/v1/cities/:id
```

**Example:**
```bash
GET /api/v1/cities/123
```

### 3. Get ZIP Codes for a City
```http
GET /api/v1/cities/zips?city=CITY&state=STATE
```

**Example:**
```bash
GET /api/v1/cities/zips?city=Dayton&state=OH
```

**Response:**
```json
{
  "success": true,
  "data": {
    "city": "Dayton",
    "state": "OH",
    "zips": [
      "45401", "45402", "45403", "45404", "45405", 
      "45406", "45409", "45410", "45414", "45415", ...
    ],
    "count": 25
  }
}
```

## Usage Pattern: ZIP Code Fallback

When searching for an address by ZIP code fails, fallback to city search:

```javascript
// 1. Try ZIP code search first
const zipResponse = await fetch('/api/v1/addresses?postcode=45401');

// 2. If no results, extract city from address and search cities
if (zipResponse.data.length === 0) {
  const cityResponse = await fetch('/api/v1/cities?city=Dayton&state=OH');
  
  // 3. Get all ZIP codes for the city
  if (cityResponse.data.length > 0) {
    const zipsResponse = await fetch('/api/v1/cities/zips?city=Dayton&state=OH');
    
    // 4. Search addresses using each ZIP code
    for (const zip of zipsResponse.data.zips) {
      const addressResponse = await fetch(`/api/v1/addresses?postcode=${zip}&street=Wyoming`);
      // Process results...
    }
  }
}
```

## Example: Finding "1424 WYOMING STREET, DAYTON, OH, 45401"

The address exists with ZIP code **45410**, not 45401. Here's how to find it using the city fallback:

```bash
# Step 1: Get all ZIP codes for Dayton, OH
curl -H "X-API-Key: YOUR_KEY" \
  "http://localhost:8080/api/v1/cities/zips?city=Dayton&state=OH"

# Response will include: 45401, 45402, ..., 45410, ...

# Step 2: Search addresses in each ZIP code
curl -H "X-API-Key: YOUR_KEY" \
  "http://localhost:8080/api/v1/addresses?street=Wyoming&city=Dayton&postcode=45410"

# Will return: 1424 WYOMING ST, DAYTON, OH, 45410
```

## Benefits

1. **Flexible Searching**: Find addresses even with incorrect ZIP codes
2. **City-Based Discovery**: Browse all addresses in a city
3. **ZIP Code Expansion**: Get all ZIP codes for a city
4. **Population Filtering**: Focus on major cities
5. **Location-Based**: Find cities near coordinates

## Performance

- **Indexes**: Full-text search with trigrams for fuzzy matching
- **Caching**: City data loaded once on startup (~31K records)
- **Fast Lookups**: Indexed by city, state, county, and coordinates
- **Efficient Queries**: Results ordered by ranking and population

## Migration

The database migration runs automatically on server start:
- Creates `cities` table
- Loads data from `uscities.csv.gz`
- Creates all necessary indexes
- Takes ~5-10 seconds on first run

## Notes

- All searches are case-insensitive
- City names use trigram fuzzy matching
- Results ordered by ranking (major cities first) then population
- ZIP codes stored as space-separated string in database
- Coordinates use decimal degrees (WGS84)
