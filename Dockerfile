FROM chainguard/go:latest AS builder

WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -o main .

# Final stage
FROM chainguard/glibc-dynamic

WORKDIR /root/

# Copy the binary from builder
COPY --from=builder /app/main .

# Copy CSV file
COPY --from=builder /app/georef-united-states-of-america-zc-point.csv .

# Expose port
EXPOSE 8080

# Run the binary
CMD ["./main"]