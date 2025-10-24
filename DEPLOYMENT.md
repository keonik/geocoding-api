# Geocoding API - Production Deployment Guide

## Coolify Deployment

This guide covers deploying the Geocoding API to Coolify, a self-hosted PaaS.

### Prerequisites

1. **Coolify Instance**: Running Coolify server
2. **Domain**: (Optional) Custom domain for your API
3. **Environment Variables**: Secure values for production

### Deployment Steps

#### 1. Clone Repository
```bash
git clone <your-repo-url>
cd geocoding-api
```

#### 2. Set Environment Variables in Coolify

In your Coolify dashboard, set these environment variables:

**Required:**
```bash
# Database
DB_USER=geocoding_user
DB_PASSWORD=<generate-secure-password>
DB_NAME=geocoding_db

# Security (CRITICAL - Generate secure keys!)
JWT_SECRET=<generate-32-char-secret>
API_SECRET_KEY=<generate-32-char-secret>
```

**Optional:**
```bash
# Ports (Coolify will auto-assign if not set)
API_PORT=8080
API_EXTERNAL_PORT=8080
DB_EXTERNAL_PORT=5432

# Performance
RATE_LIMIT_PER_MINUTE=100
MAX_CONNECTIONS=50
GO_ENV=production
```

#### 3. Deploy with Docker Compose

Coolify will automatically:
1. Build the Docker images
2. Create the database with health checks
3. Start the API service
4. Set up networking between services

### Security Best Practices

#### Generate Secure Secrets
```bash
# Generate JWT Secret (32 chars)
openssl rand -hex 32

# Generate API Secret (32 chars)
openssl rand -hex 32

# Generate DB Password (24 chars)
openssl rand -base64 24
```

#### Environment Variables
- Never commit `.env` files with secrets
- Use Coolify's environment variable management
- Rotate secrets regularly

### Health Checks

The application includes built-in health checks:

- **API**: `GET /api/v1/health`
- **Database**: PostgreSQL ready check
- **Docker**: Container health monitoring

### Monitoring

#### Endpoints to Monitor
```bash
# Health check
curl https://your-domain.com/api/v1/health

# API status
curl -H "X-API-Key: your-key" https://your-domain.com/api/v1/geocode?query=10001
```

#### Logs
```bash
# View API logs
docker logs geocoding_api

# View database logs  
docker logs geocoding_db
```

### Scaling

#### Horizontal Scaling
- Multiple API containers behind load balancer
- Single PostgreSQL instance (or read replicas)

#### Resource Requirements
```yaml
# Minimum
- API: 512MB RAM, 0.5 CPU
- DB: 1GB RAM, 1 CPU, 10GB storage

# Recommended  
- API: 1GB RAM, 1 CPU
- DB: 2GB RAM, 1 CPU, 50GB storage
```

### Backup Strategy

#### Database Backups
```bash
# Create backup
docker exec geocoding_db pg_dump -U geocoding_user geocoding_db > backup.sql

# Restore backup
docker exec -i geocoding_db psql -U geocoding_user geocoding_db < backup.sql
```

#### Volume Backups
- PostgreSQL data: `/var/lib/postgresql/data`
- API logs: Application logs in container

### Troubleshooting

#### Common Issues

1. **"No .env file found"**
   - ✅ This is normal in Docker - using environment variables
   - Check Coolify environment variable configuration

2. **Database Connection Failed**
   - Verify DB_PASSWORD matches in both services
   - Check PostgreSQL health status
   - Ensure network connectivity

3. **API Key Authentication Failed**
   - Verify JWT_SECRET is set consistently
   - Check API key is active in database
   - Validate API_SECRET_KEY configuration

#### Debug Commands
```bash
# Check running containers
docker ps

# Test database connection
docker exec geocoding_db psql -U geocoding_user -d geocoding_db -c "\dt"

# View API environment
docker exec geocoding_api env | grep -E "(DB_|JWT_|API_)"

# Test API health
curl http://localhost:8080/api/v1/health
```

### Performance Optimization

#### Database Optimization
```sql
-- Add indexes for common queries (if not already present)
CREATE INDEX IF NOT EXISTS idx_zip_codes_zip ON zip_codes(zip);
CREATE INDEX IF NOT EXISTS idx_api_keys_user_id ON api_keys(user_id);
CREATE INDEX IF NOT EXISTS idx_usage_records_api_key_id ON usage_records(api_key_id);
```

#### API Optimization
- Enable gzip compression (already configured)
- Use connection pooling (already configured)
- Monitor rate limiting effectiveness

### SSL/TLS

Coolify handles SSL automatically with Let's Encrypt:
1. Add your domain in Coolify
2. Enable SSL/TLS
3. Force HTTPS redirects

### Development vs Production

| Feature | Development | Production |
|---------|-------------|------------|
| Database | Local PostgreSQL | Docker PostgreSQL |
| Environment | .env file | Coolify env vars |
| SSL | HTTP | HTTPS (Let's Encrypt) |
| Logging | Debug level | Info level |
| Secrets | Simple values | Generated secrets |

### API Documentation

Once deployed, access API documentation at:
- Swagger UI: `https://your-domain.com/docs`
- OpenAPI spec: `https://your-domain.com/api-docs.yaml`

### Support

For deployment issues:
1. Check Coolify logs
2. Verify environment variables
3. Test health endpoints
4. Review container logs

---

🚀 **Ready to deploy!** Your production-ready geocoding API is configured for Coolify deployment.