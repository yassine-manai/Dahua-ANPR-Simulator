# ── Stage 1: build ────────────────────────────────────────────────────────────
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Copy module files first for layer caching
COPY go.mod ./

# Copy source
COPY . .

# Build a static binary (no CGO needed)
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o simulator .

# ── Stage 2: runtime ──────────────────────────────────────────────────────────
FROM alpine:3.19

WORKDIR /app

# CA certs needed for outbound HTTPS to backend (if ever TLS)
RUN apk add --no-cache ca-certificates tzdata

# Copy binary and UI assets
COPY --from=builder /app/simulator .
COPY --from=builder /app/ui        ./ui

# sim_config.json will be written here at runtime.
# Mount a volume at /app/data and symlink, or just let it write to /app.
# Default: config writes to the working dir (/app/sim_config.json).

EXPOSE 9090

ENTRYPOINT ["./simulator"]