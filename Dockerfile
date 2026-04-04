# ---- Build Stage ----
FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git

WORKDIR /src

# Cache dependency downloads
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build static binary
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w" \
    -o /bin/nexus ./cmd/nexus

# ---- Runtime Stage ----
FROM alpine:3.19

LABEL org.opencontainers.image.title="Nexus Inference Gateway" \
      org.opencontainers.image.description="Unified gateway for LLM inference APIs" \
      org.opencontainers.image.source="https://github.com/nexus-gateway/nexus" \
      org.opencontainers.image.licenses="MIT"

RUN apk add --no-cache ca-certificates curl

RUN addgroup -S nexus && adduser -S nexus -G nexus

COPY --from=builder /bin/nexus /usr/local/bin/nexus
COPY --from=builder /src/configs/nexus.yaml /etc/nexus/nexus.yaml

USER nexus

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD curl -f http://localhost:8080/health || exit 1

CMD ["nexus", "--config", "/etc/nexus/nexus.yaml"]
