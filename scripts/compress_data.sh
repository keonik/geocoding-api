#!/bin/sh
# Compress GeoJSON files for repository storage

set -e

echo "Compressing GeoJSON files..."

# Compress Ohio county files
if [ -d "oh" ]; then
    for file in oh/*.geojson; do
        if [ -f "$file" ] && [ ! -f "$file.gz" ]; then
            echo "Compressing $(basename "$file")..."
            gzip -k -9 "$file"
        fi
    done
    echo "✓ Ohio GeoJSON files compressed"
fi

# Compress CSV files
if [ -f "georef-united-states-of-america-zc-point.csv" ] && [ ! -f "georef-united-states-of-america-zc-point.csv.gz" ]; then
    echo "Compressing ZIP code CSV..."
    gzip -k -9 georef-united-states-of-america-zc-point.csv
    echo "✓ ZIP code CSV compressed"
fi

echo ""
echo "Compression complete!"
echo "Original sizes:"
du -sh oh/ 2>/dev/null || echo "  oh/: N/A"
ls -lh georef-united-states-of-america-zc-point.csv 2>/dev/null | awk '{print "  " $9 ": " $5}' || echo "  CSV: N/A"
echo ""
echo "Compressed sizes:"
du -sh oh/*.gz 2>/dev/null | tail -1 || echo "  oh/*.gz: N/A"
ls -lh georef-united-states-of-america-zc-point.csv.gz 2>/dev/null | awk '{print "  " $9 ": " $5}' || echo "  CSV.gz: N/A"
echo ""
echo "Note: Keep .gz files in git, .geojson and .csv files are in .gitignore"
