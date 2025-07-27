# Build stage
FROM golang:1.21-alpine AS builder

# Install ca-certificates for HTTPS requests
RUN apk --no-cache add ca-certificates git

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum* ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o confsync .

# Final stage
FROM alpine:latest

# Install ca-certificates for HTTPS requests and wget for health checks
RUN apk --no-cache add ca-certificates tzdata wget

# Create non-root user
RUN addgroup -S confsync && adduser -S confsync -G confsync

WORKDIR /app

# Copy the binary from builder stage
COPY --from=builder /app/confsync .

# Create sync directory
RUN mkdir -p /app/sync && chown -R confsync:confsync /app

# Switch to non-root user
USER confsync

# Default sync directory and health check port
VOLUME ["/app/sync"]
EXPOSE 8080

# Health check using the built-in endpoint
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Default command
ENTRYPOINT ["./confsync"]
CMD ["-dir", "/app/sync"]
