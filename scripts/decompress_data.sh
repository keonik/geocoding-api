#!/bin/sh
# Decompress GeoJSON and CSV files for runtime use

set -e

echo "Decompressing data files..."

# Decompress Ohio county files
if [ -d "oh" ]; then
    count=0
    for gzfile in oh/*.geojson.gz; do
        if [ -f "$gzfile" ]; then
            jsonfile="${gzfile%.gz}"
            if [ ! -f "$jsonfile" ] || [ "$gzfile" -nt "$jsonfile" ]; then
                echo "Decompressing $(basename "$gzfile")..."
                gunzip -k -f "$gzfile"
                count=$((count + 1))
            fi
        fi
    done
    if [ $count -gt 0 ]; then
        echo "✓ Decompressed $count Ohio GeoJSON files"
    else
        echo "✓ All Ohio GeoJSON files already decompressed"
    fi
fi

# Decompress CSV file
if [ -f "georef-united-states-of-america-zc-point.csv.gz" ]; then
    if [ ! -f "georef-united-states-of-america-zc-point.csv" ] || [ "georef-united-states-of-america-zc-point.csv.gz" -nt "georef-united-states-of-america-zc-point.csv" ]; then
        echo "Decompressing ZIP code CSV..."
        gunzip -k -f georef-united-states-of-america-zc-point.csv.gz
        echo "✓ ZIP code CSV decompressed"
    else
        echo "✓ ZIP code CSV already decompressed"
    fi
fi

echo "Decompression complete!"
