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

# Run Stage
# This image runs as a non-root user

FROM alpine:${ALPINE_VERSION}

RUN apk --no-cache add ca-certificates tzdata wget

USER nobody:nogroup

COPY --from=builder /app/confsync /app/confsync

WORKDIR /confsync

VOLUME ["/confsync"]
EXPOSE 8080

ENV CONFSYNC_LOCAL_DIR=/confsync

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

CMD ["/app/confsync"]
