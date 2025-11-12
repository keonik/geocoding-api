# Data Compression Scripts

This directory contains scripts for managing compressed data files in the repository.

## Overview

To keep the repository size manageable, large data files are stored compressed:
- GeoJSON files: `oh/*.geojson.gz` (~43MB compressed vs ~418MB uncompressed)
- CSV files: `georef-united-states-of-america-zc-point.csv.gz` (~1.6MB vs ~5MB)

## Scripts

### `compress_data.sh`

Compresses all GeoJSON and CSV files for repository storage.

**Usage:**
```bash
./scripts/compress_data.sh
```

**When to use:**
- Before committing new or updated data files
- After downloading fresh county data
- When adding new GeoJSON files to the `oh/` directory

**What it does:**
- Compresses all `.geojson` files in `oh/` directory using gzip -9 (maximum compression)
- Compresses the ZIP code CSV file
- Keeps original files intact (uses `-k` flag)
- Shows compression statistics

### `decompress_data.sh`

Decompresses all `.gz` files for runtime use.

**Usage:**
```bash
./scripts/decompress_data.sh
```

**When to use:**
- After cloning the repository
- When switching branches that have different data
- When `.gz` files are newer than decompressed versions
- Automatically runs during Docker build

**What it does:**
- Decompresses all `oh/*.geojson.gz` files
- Decompresses the ZIP code CSV file
- Only decompresses if target file is missing or older than `.gz` version
- Safe to run multiple times (skips already-decompressed files)

## Git Configuration

The `.gitignore` is configured to:
- ✅ **Include** compressed files (`.gz`)
- ❌ **Exclude** uncompressed files (`.geojson`, `.csv`)
- ❌ **Exclude** metadata files (`.geojson.meta`)

## Docker Integration

The Dockerfile automatically:
1. Copies compressed files from the builder stage
2. Runs `decompress_data.sh` during image build
3. Ensures all data is ready before the application starts

## Workflow

### For Contributors

**After adding/updating data:**
```bash
# 1. Compress the files
./scripts/compress_data.sh

# 2. Stage only the .gz files
git add oh/*.geojson.gz
git add georef-united-states-of-america-zc-point.csv.gz

# 3. Commit
git commit -m "Update Ohio county data"
```

**After cloning/pulling:**
```bash
# Decompress for local development
./scripts/decompress_data.sh

# Or just run the app (Docker handles it automatically)
docker compose up
```

## File Sizes

| Type | Uncompressed | Compressed | Ratio |
|------|-------------|------------|-------|
| Ohio GeoJSON files | ~418 MB | ~43 MB | 90% |
| ZIP code CSV | ~5 MB | ~1.6 MB | 68% |
| **Total** | **~423 MB** | **~45 MB** | **89%** |

## Notes

- Compressed files use maximum compression (`gzip -9`)
- Original files are preserved during compression (`-k` flag)
- Decompression is idempotent (safe to run multiple times)
- The app expects decompressed files at runtime
