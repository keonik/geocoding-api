#!/bin/bash

# Test GDAL installation and shapefile conversion

echo "Testing GDAL installation..."

# Check if ogr2ogr is installed
if ! command -v ogr2ogr &> /dev/null; then
    echo "❌ ogr2ogr not found!"
    echo ""
    echo "Please install GDAL:"
    echo "  macOS:    brew install gdal"
    echo "  Ubuntu:   sudo apt-get install gdal-bin"
    echo "  Windows:  Download from https://gdal.org/download.html"
    exit 1
fi

# Check version
echo "✅ GDAL is installed:"
ogr2ogr --version

echo ""
echo "Testing shapefile conversion..."

# Create test directory
TEST_DIR="./test_conversion"
mkdir -p "$TEST_DIR"

# Download a sample Ohio county address file
echo "Downloading Adams County address data..."
curl -o "$TEST_DIR/ADA_ADDS.zip" "https://gis1.oit.ohio.gov/LBRS/_downloads/ADA_ADDS.zip"

if [ $? -ne 0 ]; then
    echo "❌ Failed to download test data"
    rm -rf "$TEST_DIR"
    exit 1
fi

# Extract the ZIP
echo "Extracting ZIP file..."
unzip -q "$TEST_DIR/ADA_ADDS.zip" -d "$TEST_DIR"

# Find the shapefile
SHAPEFILE=$(find "$TEST_DIR" -name "*.shp" | head -n 1)

if [ -z "$SHAPEFILE" ]; then
    echo "❌ No shapefile found in ZIP"
    rm -rf "$TEST_DIR"
    exit 1
fi

echo "Found shapefile: $SHAPEFILE"

# Convert to GeoJSON
echo "Converting to GeoJSON..."
ogr2ogr -f GeoJSON -t_srs EPSG:4326 "$TEST_DIR/adams-addresses.geojson" "$SHAPEFILE"

if [ $? -eq 0 ]; then
    echo "✅ Conversion successful!"
    
    # Show sample of GeoJSON
    echo ""
    echo "Sample of converted GeoJSON:"
    head -50 "$TEST_DIR/adams-addresses.geojson"
    
    # Count features
    FEATURE_COUNT=$(grep -c '"type": "Feature"' "$TEST_DIR/adams-addresses.geojson")
    echo ""
    echo "Total features: $FEATURE_COUNT"
else
    echo "❌ Conversion failed"
    rm -rf "$TEST_DIR"
    exit 1
fi

# Cleanup
echo ""
echo "Cleaning up test files..."
rm -rf "$TEST_DIR"

echo ""
echo "✅ All tests passed! GDAL is ready for use."
