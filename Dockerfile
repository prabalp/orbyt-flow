FROM golang:1.23-alpine AS builder

WORKDIR /app

# Install ca-certificates for HTTPS requests
RUN apk add --no-cache ca-certificates

# Download dependencies first (better layer caching)
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o orbyt-flow ./cmd/server

# Runtime image
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /app/orbyt-flow /usr/local/bin/orbyt-flow

# Create non-root user
RUN adduser -D -H -s /sbin/nologin orbyt
USER orbyt

EXPOSE 8085

VOLUME ["/data"]

ENV FLOWENGINE_DATA_DIR=/data \
    PORT=8085

ENTRYPOINT ["orbyt-flow"]
