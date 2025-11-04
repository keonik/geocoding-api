# 🚀 DEPLOYMENT CHECKLIST

## Pre-Deployment Requirements

### ✅ Environment Variables (CRITICAL)
Set these in your Coolify environment:

```bash
# Database (Generate secure password)
DB_USER=geocoding_user
DB_PASSWORD=<generate-secure-24-char-password>
DB_NAME=geocoding_db

# Security (Generate with: openssl rand -hex 32)
JWT_SECRET=<generate-32-char-secret>
API_SECRET_KEY=<generate-32-char-secret>

# Application
GO_ENV=production
API_PORT=8080

# CORS (Optional - defaults to geocode.jfay.dev)
# CORS_ORIGINS=https://geocode.jfay.dev,https://your-other-domain.com
```

### 🔐 Security Commands
```bash
# Generate secure password (24 chars)
openssl rand -base64 24

# Generate JWT secret (32 chars hex)
openssl rand -hex 32

# Generate API secret (32 chars hex)  
openssl rand -hex 32
```

## 🛠️ Deployment Issues Fixed

### ✅ Volume Mount Conflicts Removed
- **Problem**: Docker volumes overriding built-in files
- **Fix**: Removed `./static` and `./docs` volume mounts
- **Result**: Uses files built into Docker image

### ✅ Security Hardening  
- **Problem**: Postgres port exposed externally
- **Fix**: Commented out external port mapping
- **Result**: Database only accessible internally

### ✅ Environment Validation
- **Problem**: No warnings for weak defaults
- **Fix**: Added production security warnings
- **Result**: Alerts when using default secrets

### ✅ CORS Security Configuration
- **Problem**: Permissive CORS allowing all origins
- **Fix**: Environment-based CORS with domain restrictions
- **Result**: Production locked to https://geocode.jfay.dev

### ✅ CSV File Optimization
- **Problem**: CSV mounted as volume AND copied to image
- **Fix**: File already in Docker image, no volume needed
- **Result**: Cleaner deployment, faster startup

## 🎯 Coolify Deployment Steps

1. **Set Environment Variables** in Coolify dashboard
2. **Push your code** to Git repository  
3. **Import project** in Coolify
4. **Use docker-compose.yml** for deployment
5. **Check logs** for security warnings

## ⚠️ Production Warnings

The app will now warn you if:
- Using default `JWT_SECRET` in production
- Using default `API_SECRET_KEY` in production
- Database connection issues
- Missing critical environment variables

## 🧪 Test Before Deploy

```bash
# Local test with production settings
GO_ENV=production docker-compose up --build

# Check for security warnings in logs
docker-compose logs geocoding-api | grep WARNING
```

## 📊 Post-Deployment Verification

1. **Health check**: `https://your-domain.com/api/v1/health`
2. **Web interface**: `https://your-domain.com`
3. **API docs**: `https://your-domain.com/docs`
4. **Create test account** and API key
5. **Test geocoding**: Use API key in Swagger UI

---

✅ **Your geocoding API is now deployment-ready!**