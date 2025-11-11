# Docker Deployment with On-Demand Downloads

## Overview

With the new on-demand download system, Docker deployments will automatically fetch Ohio county data during the first startup, eliminating the need to store large files in the Git repository or Docker image.

## How It Works in Docker

### 1. Container Startup Process

When the Docker container starts:

1. **Application Launch**: The Go application starts and connects to the database
2. **Migration Check**: Database migrations run automatically
3. **Download Trigger**: If Ohio data migration hasn't been applied, it triggers the download
4. **File Download**: System downloads Ohio county data from OpenAddresses repository
5. **Data Processing**: Downloaded files are processed and data is loaded into the database
6. **Ready**: Application becomes ready to serve requests

### 2. Docker Container Changes

The updated Dockerfile:
- ✅ **Removes** static `oh/` directory copy
- ✅ **Creates** empty `oh/` and `cache/` directories with proper permissions
- ✅ **Includes** network utilities (`wget`, `curl`) for downloads
- ✅ **Sets** proper user permissions for file creation

### 3. Network Requirements

The Docker container needs:
- **Outbound HTTPS access** to `raw.githubusercontent.com` for OpenAddresses configs
- **Outbound HTTP access** to Ohio government data sources (when available)
- **No special firewall rules** - uses standard HTTP/HTTPS ports

### 4. Storage Considerations

- **Temporary Storage**: Downloads use `/app/cache/` (cleaned up after processing)
- **Persistent Storage**: Processed data goes into PostgreSQL database
- **No Volume Mounts**: All data processing happens in-container

## Deployment Scenarios

### First-Time Deployment

```bash
# Build and start containers
docker-compose up -d

# Monitor download progress
docker logs -f geocoding_api
```

Expected log output:
```
2025/11/11 20:00:00 Running database migrations...
2025/11/11 20:00:01 Attempting to download real Ohio county data...
2025/11/11 20:00:02 Trying source: OpenAddresses GitHub Raw
2025/11/11 20:00:03 Successfully processed 88 OpenAddresses configurations
2025/11/11 20:00:04 Loading Ohio address data from GeoJSON files...
2025/11/11 20:00:05 Successfully loaded 0 total address records from Ohio counties
2025/11/11 20:00:06 Server started on port 8080
```

### Production Deployment

For production deployments (Coolify, etc.):

1. **Environment Variables**: Set required environment variables
2. **Network Access**: Ensure outbound internet access
3. **Resource Allocation**: Allow extra CPU/memory during first startup for downloads
4. **Startup Time**: First deployment may take 2-3 minutes longer due to downloads

### Re-deployments

Subsequent deployments:
- **Skip Downloads**: Migration already applied, no downloads needed
- **Fast Startup**: Normal startup time since data is in database
- **Cache Cleanup**: Cache directory is ephemeral and cleared on restart

## Troubleshooting

### Download Failures

If downloads fail, the system:
1. **Logs Warning**: Shows download failure in logs
2. **Creates Placeholders**: Creates empty placeholder files
3. **Continues Startup**: Application starts normally
4. **Graceful Degradation**: System works with reduced functionality

### Network Issues

Common network issues:
```bash
# Check container network access
docker exec geocoding_api ping -c 1 raw.githubusercontent.com

# Check download logs
docker logs geocoding_api | grep -i download

# Manual download test
docker exec geocoding_api wget -O- https://raw.githubusercontent.com/openaddresses/openaddresses/master/sources/us/oh/adams.json
```

### Permission Issues

If permission errors occur:
```bash
# Check directory permissions
docker exec geocoding_api ls -la /app/

# Check user context
docker exec geocoding_api whoami

# Check write permissions
docker exec geocoding_api touch /app/oh/test.txt
```

## Benefits in Docker

1. **Smaller Images**: No large files in Docker images
2. **Faster Builds**: No need to copy large datasets during build
3. **Always Current**: Downloads latest data configurations
4. **Flexible Sources**: Easy to update data sources without rebuilding images
5. **Clean Deployments**: No Git LFS complexity in CI/CD

## Environment Variables

No additional environment variables required for download functionality. The system uses sensible defaults:

- `CACHE_DIR`: Defaults to `./cache`
- `DATA_DIR`: Defaults to `./oh`
- `DOWNLOAD_TIMEOUT`: 30 minutes (hardcoded)
- `CACHE_DURATION`: 24 hours (hardcoded)

## Monitoring

Monitor download progress through:
- **Application Logs**: Standard Docker logs
- **Health Checks**: Built-in health endpoint
- **Database Metrics**: Migration completion status

The system is designed to be self-healing and will retry downloads on subsequent restarts if needed.