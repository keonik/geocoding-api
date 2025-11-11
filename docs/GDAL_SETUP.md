# GDAL Setup for Shapefile Conversion

This document explains how to set up GDAL for automatic shapefile-to-GeoJSON conversion of Ohio address data.

## What is GDAL?

GDAL (Geospatial Data Abstraction Library) is an open-source library for working with geospatial data formats. We use the `ogr2ogr` command-line tool from GDAL to convert shapefiles from the Ohio LBRS site into GeoJSON format.

## Installation

### macOS (using Homebrew)

```bash
brew install gdal
```

### Ubuntu/Debian

```bash
sudo apt-get update
sudo apt-get install gdal-bin
```

### Windows

1. Download GDAL from: https://gdal.org/download.html
2. Or use OSGeo4W installer: https://trac.osgeo.org/osgeo4w/
3. Add GDAL to your PATH environment variable

### Docker

If running in Docker, add to your Dockerfile:

```dockerfile
RUN apt-get update && \
    apt-get install -y gdal-bin && \
    rm -rf /var/lib/apt/lists/*
```

## Verify Installation

After installation, verify GDAL is working:

```bash
ogr2ogr --version
```

You should see output like:
```
GDAL 3.8.0, released 2023/11/13
```

## Test Conversion

Run the test script to verify everything works:

```bash
./scripts/test_gdal.sh
```

This will:
1. Check if GDAL is installed
2. Download a sample Ohio county address file
3. Extract and convert it to GeoJSON
4. Display statistics about the conversion

## How It Works

### 1. Download Phase

The application downloads ZIP files from Ohio LBRS:
```
https://gis1.oit.ohio.gov/LBRS/_downloads/ADA_ADDS.zip
```

Where `ADA` is the 3-letter county code (e.g., ADA = Adams County).

### 2. Extraction Phase

The ZIP file is extracted to a temporary directory. It typically contains:
- `*.shp` - Shapefile (geometry data)
- `*.shx` - Shape index
- `*.dbf` - Attribute database
- `*.prj` - Projection information

### 3. Conversion Phase

Using ogr2ogr:
```bash
ogr2ogr -f GeoJSON -t_srs EPSG:4326 output.geojson input.shp
```

Parameters:
- `-f GeoJSON` - Output format
- `-t_srs EPSG:4326` - Transform to WGS84 coordinate system
- Output and input file paths

### 4. Result

A GeoJSON file with structure:
```json
{
  "type": "FeatureCollection",
  "features": [
    {
      "type": "Feature",
      "properties": {
        "hash": "...",
        "number": "123",
        "street": "Main St",
        "city": "Columbus",
        "postcode": "43215"
      },
      "geometry": {
        "type": "Point",
        "coordinates": [-82.999, 39.971]
      }
    }
  ]
}
```

## Troubleshooting

### "ogr2ogr: command not found"

GDAL is not installed or not in your PATH. Follow the installation instructions above.

### "Cannot open data source"

The shapefile may be corrupted or the ZIP extraction failed. Check:
1. ZIP file downloaded completely
2. ZIP file is not corrupted
3. Temporary directory has write permissions

### "Unsupported geometry type"

The shapefile may contain geometry types other than Points. This is unusual for address data but can happen. Check the shapefile contents:

```bash
ogrinfo -al input.shp | head -20
```

### Conversion is slow

Large county files can take several minutes to convert. This is normal for datasets with 100,000+ addresses. The conversion is cached, so it only happens once per county.

## Manual Conversion

If automatic conversion fails, you can convert manually:

```bash
# Download
curl -O https://gis1.oit.ohio.gov/LBRS/_downloads/ADA_ADDS.zip

# Extract
unzip ADA_ADDS.zip

# Convert
ogr2ogr -f GeoJSON -t_srs EPSG:4326 \
  oh/adams-addresses-county.geojson \
  ADA_ADDS.shp

# Verify
head oh/adams-addresses-county.geojson
```

## Performance Considerations

### Caching

The application caches:
- Downloaded ZIP files (24 hours)
- Converted GeoJSON files (24 hours)

This prevents unnecessary re-downloads and re-conversions.

### Disk Space

- ZIP files: ~1-50 MB per county
- GeoJSON files: ~5-200 MB per county (uncompressed)
- Total for all 88 Ohio counties: ~5-15 GB

### Processing Time

- Small county (< 10,000 addresses): 5-30 seconds
- Medium county (10,000-50,000 addresses): 30-120 seconds
- Large county (> 50,000 addresses): 2-10 minutes

## Alternative: Pre-converted Data

If GDAL installation is problematic, consider:

1. **OpenAddresses**: May have pre-converted data
   - https://batch.openaddresses.io/
   - https://results.openaddresses.io/

2. **Manual batch conversion**: Convert all counties once, commit GeoJSON to repo
   - Run conversion script on a machine with GDAL
   - Commit resulting GeoJSON files
   - Trade storage space for processing time

## References

- GDAL Documentation: https://gdal.org/
- ogr2ogr Manual: https://gdal.org/programs/ogr2ogr.html
- Ohio LBRS Data: https://gis1.oit.ohio.gov/LBRS/
- OpenAddresses: https://openaddresses.io/
