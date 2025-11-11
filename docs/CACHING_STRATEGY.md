# Caching Strategy for Ohio Address Data

This document explains how the application caches downloaded and converted data to avoid unnecessary downloads and processing.

## Three-Level Caching

The system uses a three-level caching strategy to minimize redundant work:

### 1. Database-Level Cache (Permanent)

**Location**: PostgreSQL `ohio_addresses` table

**When checked**: At migration time (every application start)

**Behavior**:
```go
// In loadOhioAddressData()
SELECT COUNT(*) FROM ohio_addresses
```

- ✅ **If count > 0**: Skip all downloads and processing
- ❌ **If count = 0**: Proceed with download/conversion

**Result**: Once data is loaded into the database, it's **never re-downloaded** unless you:
- Drop the table
- Delete migration records
- Manually truncate the table

### 2. File-Level Cache (24 hours)

**Location**: 
- GeoJSON files: `oh/*.geojson` 
- ZIP files: `cache/*.zip`

**When checked**: When downloading/converting files

**Behavior**:
```go
if rdd.isCached(zipPath, 24*time.Hour) {
    fmt.Printf("Using cached ZIP file for %s\n", county)
}
```

**File Age Check**:
```go
func (rdd *RealDataDownloader) isCached(filePath string, maxAge time.Duration) bool {
    info, err := os.Stat(filePath)
    if err != nil {
        return false // File doesn't exist
    }
    return time.Since(info.ModTime()) < maxAge // Check if recent
}
```

**Result**: Files are cached for 24 hours from their modification time:
- ✅ **If file exists and < 24 hours old**: Use cached version
- ❌ **If file missing or > 24 hours old**: Re-download/convert

### 3. Migration-Level Cache (Permanent)

**Location**: PostgreSQL `schema_migrations` table

**When checked**: At application start

**Behavior**:
```sql
SELECT COUNT(*) FROM schema_migrations WHERE version = 8
```

- ✅ **If migration 8 applied**: Skip entire migration (including download)
- ❌ **If migration 8 not applied**: Run migration and load data

**Result**: Migration only runs **once** unless you manually reset it.

## Complete Flow Chart

```
Application Starts
        ↓
Check: Migration 8 Applied?
        ↓
    YES → Skip everything (DONE) ✅
        ↓
    NO  → Continue
        ↓
Check: ohio_addresses table has data?
        ↓
    YES → Skip download/load (DONE) ✅
        ↓
    NO  → Continue
        ↓
For each Ohio county:
        ↓
    Check: GeoJSON file exists and < 24hrs old?
        ↓
        YES → Use cached file, skip to next county
        ↓
        NO  → Continue
        ↓
    Check: ZIP file exists and < 24hrs old?
        ↓
        YES → Use cached ZIP, convert only
        ↓
        NO  → Download ZIP from Ohio LBRS
        ↓
    Convert ZIP → GeoJSON (using ogr2ogr)
        ↓
Load GeoJSON into database
        ↓
Mark migration as complete
```

## What Triggers Re-Downloads?

### Normal Usage (Nothing triggers re-download)
- ✅ Restart application → Uses database data
- ✅ Deploy new version → Uses database data  
- ✅ Scale up containers → Each uses shared database

### Manual Data Refresh
To force a complete refresh:

```bash
# 1. Connect to database
docker compose exec db psql -U postgres -d geocoding_db

# 2. Clear migration and data
DELETE FROM schema_migrations WHERE version = 8;
TRUNCATE TABLE ohio_addresses;

# 3. Clear cached files
rm -rf oh/* cache/*

# 4. Restart application
docker compose restart app
```

### Partial Refresh (Files Only)
To re-download files but keep database data:

```bash
# Clear files
rm -rf oh/* cache/*

# Files will be re-downloaded on next access
# But database won't be reloaded (it already has data)
```

## Cache Benefits

### Storage Efficiency
- **Database**: One copy shared across all containers
- **Files**: Optional, only needed during initial load
- **Can delete files after load**: `rm -rf oh/* cache/*` (saves ~15GB)

### Time Efficiency
- **First run**: 10-60 minutes (download + convert + load all counties)
- **Subsequent runs**: < 1 second (database check only)
- **With file cache**: 5-30 minutes (convert + load only)

### Network Efficiency
- **Downloads once**: ZIP files cached for 24 hours
- **Bandwidth saved**: ~500MB-2GB per county set
- **Ohio LBRS friendly**: Respects their bandwidth

## Cache Configuration

Currently hardcoded, but could be made configurable:

```go
// Potential environment variables
CACHE_MAX_AGE=24h           // File cache duration
CACHE_DIR=/app/cache        // Cache directory location  
SKIP_CACHE=false            // Force re-download (for debugging)
```

## Development vs Production

### Development
```bash
# Frequent restarts → Database persists
# Files can be deleted → Will re-download if needed
# Fast iteration → Database cache crucial
```

### Production
```bash
# Rare restarts → Database persists
# Files optional → Can delete after initial load
# Multiple replicas → All share same database
```

## Monitoring Cache Hits

To see if caching is working, check the logs:

### Cache Hit
```
Database already contains 2847392 Ohio address records, skipping download/load
```

### Cache Miss (First Run)
```
Loading Ohio address data from GeoJSON files...
Processing adams-addresses-county.geojson (Adams)
Downloading real data from Ohio LBRS for adams...
Converting shapefile adams to GeoJSON...
Loaded 18472 records from Adams
```

### Partial Cache (Files Cached)
```
Loading Ohio address data from GeoJSON files...  
Using cached GeoJSON file for adams
Loaded 18472 records from Adams
```

## Cache Invalidation

### Automatic (File Age)
- Files older than 24 hours are considered stale
- Next access will re-download/convert

### Manual (Force Refresh)
See "What Triggers Re-Downloads?" above

### Recommended Schedule
- **Production**: Manual refresh quarterly or when Ohio publishes updates
- **Development**: No refresh needed (unless testing download logic)

## Best Practices

1. **Don't delete database data** - It's your primary cache
2. **Can delete files** - They're only needed during initial load
3. **Monitor disk space** - Cache directory can grow to ~15GB
4. **Set up alerts** - Notify if initial load takes too long (indicates cache miss)
5. **Document refreshes** - Track when you manually refresh data

## Troubleshooting

### "Data keeps re-downloading on restart"
Check: Is database persisting between restarts?
- Docker volume might not be mounted correctly
- Database container might be recreating

### "Files not caching"
Check: Are file timestamps being preserved?
- Volume mounts should preserve modification times
- Container rebuilds might reset file timestamps

### "Migration running every time"
Check: Is `schema_migrations` table persisting?
- Database initialization might be running each time
- Connection to wrong database instance

## Summary

**The short answer**: 

🎯 **No, files are NOT downloaded every time!**

1. **First run only**: Downloads and loads everything
2. **Every subsequent run**: Checks database, finds data, skips everything
3. **File cache**: Even if database was empty, files cached for 24 hours
4. **ZIP cache**: Downloaded ZIP files reused for 24 hours

**Your database is the permanent cache. Once loaded, never downloads again.**
