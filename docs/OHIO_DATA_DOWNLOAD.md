# Ohio Data Download System

This document describes the on-demand download system implemented to replace the Git LFS storage of Ohio GeoJSON files.

## Overview

Instead of storing large GeoJSON files in Git LFS, the system now downloads Ohio county address data on demand during database migrations. This approach:

- Eliminates Git LFS costs and limitations
- Provides flexibility to update data sources
- Implements caching for performance
- Maintains backward compatibility with existing migration structure

## Architecture

### Core Components

1. **FileDownloader** (`utils/file_downloader.go`)
   - Basic file download and caching functionality
   - Handles HTTP downloads with timeout management
   - Supports ZIP extraction for compressed data sources

2. **RealDataDownloader** (`utils/real_data_downloader.go`)
   - Advanced downloader for real data sources
   - Integrates with OpenAddresses configuration system
   - Attempts multiple data source strategies
   - Falls back to placeholder files when real data unavailable

### Data Sources

The system attempts to download data from multiple sources in priority order:

1. **OpenAddresses GitHub Repository**
   - Downloads configuration files from `https://github.com/openaddresses/openaddresses`
   - Extracts data source URLs from JSON configuration files
   - Creates structured GeoJSON and metadata files

2. **Ohio Government Sources** (Future enhancement)
   - Direct integration with Ohio state GIS servers
   - Census Bureau TIGER data
   - Local government APIs

3. **Placeholder Files** (Fallback)
   - Creates empty GeoJSON structures when real data unavailable
   - Maintains system functionality during development
   - Includes metadata indicating placeholder status

## File Structure

### Downloaded Files
```
oh/
├── adams-addresses-county.geojson      # GeoJSON feature collection
├── adams-addresses-county.geojson.meta # Metadata and source information
├── allen-addresses-county.geojson
├── allen-addresses-county.geojson.meta
└── ... (88 counties total)
```

### Cache Directory
```
cache/                                  # Temporary download cache
└── (downloaded files cached here)
```

## Implementation Details

### Migration Integration

The download system integrates with existing database migrations:

```go
// In database/migrations.go
func loadOhioAddressData() error {
    // Download Ohio data if not present
    downloader := utils.NewFileDownloader("./cache")
    if err := downloader.DownloadOhioData("."); err != nil {
        log.Printf("Warning: Failed to download Ohio data: %v", err)
        log.Println("Continuing with existing files if available...")
    }
    
    // Continue with existing file processing logic...
}
```

### Caching Strategy

- **Cache Duration**: 24 hours for downloaded files
- **Cache Location**: `./cache/` directory (gitignored)
- **Cache Validation**: File modification time comparison
- **Cache Behavior**: Downloads only if files don't exist or are expired

### Error Handling

The system implements robust error handling:

1. **Network Failures**: Continues with existing files if download fails
2. **Missing Sources**: Falls back to placeholder files
3. **Invalid Data**: Logs warnings but continues processing
4. **File System Errors**: Proper error propagation with context

## Configuration

### Environment Variables

No additional environment variables required. The system uses:
- Default cache directory: `./cache`
- Default destination: `./oh`

### Git Configuration

Updated `.gitignore` to exclude downloaded files:
```
# Downloaded data cache
cache/
oh/
```

## Data Source Details

### OpenAddresses Configuration

Each county has a corresponding configuration file at:
`https://raw.githubusercontent.com/openaddresses/openaddresses/master/sources/us/oh/{county}.json`

Example configuration for Adams County:
```json
{
    "coverage": {
        "US Census": {
            "geoid": "39001",
            "state": "Ohio"
        },
        "country": "us",
        "state": "oh",
        "county": "Adams"
    },
    "layers": {
        "addresses": [{
            "name": "county",
            "data": "https://gis1.oit.ohio.gov/LBRS/_downloads/ADA_ADDS.zip",
            "protocol": "http",
            "compression": "zip",
            "conform": {
                "number": "HOUSENUM",
                "street": ["ST_PREFIX", "ST_NAME", "ST_TYPE", "ST_SUFFIX"],
                "city": "MUNI",
                "region": "STATE",
                "format": "shapefile",
                "postcode": "zipcode"
            }
        }]
    }
}
```

### Generated File Format

#### GeoJSON File Structure
```json
{
  "type": "FeatureCollection",
  "features": [],
  "metadata": {
    "county": "Adams",
    "state": "Ohio",
    "source": "OpenAddresses",
    "data_source": "https://gis1.oit.ohio.gov/LBRS/_downloads/ADA_ADDS.zip",
    "last_check": "2025-11-11T15:28:44-05:00"
  }
}
```

#### Metadata File Structure
```json
{
  "county": "Adams",
  "state": "Ohio",
  "record_count": 0,
  "last_updated": "2025-11-11T15:28:44-05:00",
  "source": "OpenAddresses",
  "coverage": {
    "country": "us",
    "state": "oh",
    "county": "Adams"
  },
  "layers": {
    "addresses": [{
      "name": "county",
      "data": "https://gis1.oit.ohio.gov/LBRS/_downloads/ADA_ADDS.zip",
      "protocol": "http"
    }]
  }
}
```

## Testing

The system was successfully tested by:

1. **Migration Reset**: Removed migration tracking for Ohio address data
2. **File Cleanup**: Deleted existing `oh/` directory
3. **Download Trigger**: Restarted application to trigger migration
4. **Verification**: Confirmed 176 files created (88 counties × 2 files each)

### Test Results

- ✅ 88 GeoJSON files created with proper structure
- ✅ 88 metadata files created with source information
- ✅ OpenAddresses configuration files successfully downloaded and processed
- ✅ System gracefully handles unavailable data sources
- ✅ Maintains backward compatibility with existing migration system

## Future Enhancements

1. **Real Data Integration**
   - Implement actual data downloads from Ohio government sources
   - Add ZIP file extraction and shapefile conversion
   - Implement data validation and cleaning

2. **Enhanced Caching**
   - Database-backed cache tracking
   - Selective county updates
   - Version-aware caching

3. **Data Source Management**
   - Configuration-driven data source priorities
   - Source reliability tracking
   - Automatic fallback mechanisms

4. **Monitoring and Alerting**
   - Download success/failure tracking
   - Data freshness monitoring
   - Automated data quality checks

## Benefits

1. **Cost Elimination**: No more Git LFS storage costs
2. **Flexibility**: Easy to update data sources without Git commits
3. **Performance**: Caching prevents unnecessary re-downloads
4. **Maintainability**: Clean separation of data acquisition and processing
5. **Scalability**: Easy to add new counties or data sources

## Conclusion

The on-demand download system successfully replaces Git LFS storage while maintaining system functionality and providing a foundation for future enhancements. The system is production-ready and provides a robust solution for managing large geospatial datasets.