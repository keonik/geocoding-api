# Async Database Migrations

## Overview

By default, the application waits for all database migrations to complete before starting the server. This ensures data consistency but can cause long startup times when migrations involve updating millions of records.

The async migration feature allows you to start the server immediately while migrations run in the background.

## When to Use Async Migrations

✅ **Use async migrations when:**
- You have long-running data migrations (e.g., updating full_address on 4M+ records)
- You need the API to be available quickly for health checks
- You're okay with certain features being limited during migration
- You're running in production with proper monitoring

❌ **Don't use async migrations when:**
- Migrations are adding new tables/columns that the app immediately needs
- You need guaranteed schema consistency before serving requests
- You're in development and want immediate error feedback

## How to Enable

### Environment Variable

Set `RUN_MIGRATIONS_ASYNC=true` in your environment:

```bash
# Docker Compose
RUN_MIGRATIONS_ASYNC=true docker-compose up

# Docker run
docker run -e RUN_MIGRATIONS_ASYNC=true geocoding-api

# Direct execution
RUN_MIGRATIONS_ASYNC=true ./main
```

### Docker Compose

```yaml
environment:
  RUN_MIGRATIONS_ASYNC: "true"
```

### Coolify / Cloud Platform

Add environment variable:
```
RUN_MIGRATIONS_ASYNC=true
```

## Monitoring Migration Status

### Health Check Endpoint

The `/api/v1/health` endpoint includes migration status:

**During migration:**
```json
{
  "status": "healthy",
  "service": "geocoding-api",
  "version": "1.0.0",
  "migrations_running": true,
  "note": "Database migrations in progress - some features may be limited"
}
```

**After successful migration:**
```json
{
  "status": "healthy",
  "service": "geocoding-api",
  "version": "1.0.0"
}
```

**If migration fails:**
```json
{
  "status": "healthy",
  "service": "geocoding-api",
  "version": "1.0.0",
  "migration_error": "error details here",
  "note": "Migration error occurred - check logs"
}
```

### Server Logs

Migrations log their progress to stdout:

```
2025/11/23 00:50:08 Running migrations asynchronously - server will start immediately
2025/11/23 00:50:08 Server starting while migrations run in background...
2025/11/23 00:50:08 Starting migrations in background...
2025/11/23 00:50:08 Running database migrations...
2025/11/23 00:50:08 Migration 16: Expand street abbreviations in full_address column
2025/11/23 00:55:30 Street abbreviations expanded in full_address column
2025/11/23 00:55:30 All migrations completed successfully
2025/11/23 00:55:30 Background migrations completed successfully
```

## Example: Migration 16 (Street Abbreviations)

Migration 16 updates ~4M address records to expand abbreviations (DR → Drive, ST → Street, etc.). This can take 5-10 minutes.

**Without async migrations:**
```bash
# Server blocks for 5-10 minutes
docker-compose up
# ... waiting ...
# ... waiting ...
# Server finally starts
```

**With async migrations:**
```bash
RUN_MIGRATIONS_ASYNC=true docker-compose up
# Server starts in ~10 seconds
# Migrations run in background
# Check /api/v1/health to monitor progress
```

## Best Practices

1. **Enable in Production Only**: Use synchronous migrations in development for immediate feedback

2. **Monitor Health Endpoint**: Poll `/api/v1/health` to track migration progress

3. **Check Logs**: Always review server logs after deployment to ensure migrations completed

4. **Plan Rollback**: If migration fails, you may need to manually fix the database state

5. **Test First**: Always test long-running migrations in staging with async mode before production

## Troubleshooting

**Server starts but features don't work:**
- Check `/api/v1/health` for `migrations_running: true`
- Wait for migrations to complete
- Check logs for errors

**Migration fails silently:**
- Check logs: `docker logs geocoding_api`
- Look for `migration_error` in health check response
- May need to manually roll back and fix

**How long will it take?**
- Migration 16: ~5-10 minutes for 4M records
- Other migrations: typically < 1 second
- Monitor logs for progress updates
