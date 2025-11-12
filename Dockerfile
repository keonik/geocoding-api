# Build stage
FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app

# Copy go mod files first for better caching
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags='-w -s -extldflags "-static"' \
    -o main .

# Production stage
FROM alpine:latest

# Install runtime dependencies including GDAL for shapefile conversion
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    wget \
    curl \
    gdal \
    gdal-tools

# Create non-root user
RUN addgroup -g 1001 -S appgroup && \
    adduser -u 1001 -S appuser -G appgroup

WORKDIR /app

# Create directories first
RUN mkdir -p /app/oh /app/cache /app/scripts

# Copy binary from builder
COPY --from=builder /app/main ./main

# Copy scripts first
COPY --from=builder /app/scripts/decompress_data.sh ./scripts/

# Copy compressed data files
COPY --from=builder /app/georef-united-states-of-america-zc-point.csv.gz* ./
COPY --from=builder /app/oh/ ./oh/

# Copy other runtime files
COPY --from=builder /app/static ./static
COPY --from=builder /app/docs ./docs
COPY --from=builder /app/api-docs.yaml ./api-docs.yaml

# Decompress data files and set permissions
RUN chmod +x /app/scripts/decompress_data.sh && \
    /app/scripts/decompress_data.sh || echo "No compressed files to decompress" && \
    chown -R appuser:appgroup /app

# Set environment variables for production
ENV ENV=production
ENV GO_ENV=production

# Switch to non-root user
USER appuser

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=30s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/api/v1/health || exit 1

# Run the application
CMD ["./main"]