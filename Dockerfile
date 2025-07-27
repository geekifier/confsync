ARG GO_VERSION=1.24
ARG ALPINE_VERSION=3.22

# Build stage
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine${ALPINE_VERSION} AS builder
RUN apk --no-cache add ca-certificates git
WORKDIR /app
COPY go.mod go.sum* ./
RUN go mod download
COPY . .

ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -a -installsuffix cgo -o confsync .

# Final stage
FROM alpine:${ALPINE_VERSION}
RUN apk --no-cache add ca-certificates tzdata wget
RUN addgroup -S confsync && adduser -S confsync -G confsync

USER confsync
WORKDIR /app
COPY --from=builder /app/confsync .

VOLUME ["/config"]
EXPOSE 8080

ENV CONFSYNC_LOCAL_DIR=/config

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

ENTRYPOINT ["./confsync"]
