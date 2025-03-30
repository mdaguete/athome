# Frontend build stage
FROM docker.io/library/node:20-alpine AS frontend-builder

WORKDIR /app/frontend

# Install dependencies first (better layer caching)
COPY frontend/package*.json ./
RUN npm ci

# Copy frontend source files individually for better caching
COPY frontend/index.html ./
COPY frontend/vite.config.js ./
COPY frontend/src/counter.js ./src/
COPY frontend/src/javascript.svg ./src/
COPY frontend/src/main.js ./src/
COPY frontend/src/style.css ./src/
COPY frontend/src/js ./src/js

# Build frontend
RUN npm run build && \
    mkdir -p /app/public && \
    cp -r dist/* /app/public/

# Go builder stage
FROM docker.io/library/golang:1.23-alpine AS backend-builder

# Install build dependencies
RUN apk add --no-cache git build-base

WORKDIR /app

# Cache dependencies layer
COPY go.mod go.sum ./
RUN go mod download

# Copy frontend build from previous stage
COPY --from=frontend-builder /app/public ./public

# Copy backend source
COPY . .

# Build with optimizations
RUN CGO_ENABLED=0 go build \
    -ldflags="-w -s" \
    -trimpath \
    -o athome .

# Final stage - using distroless for minimal attack surface
FROM gcr.io/distroless/static-debian12:nonroot

# Copy binary and static files
COPY --from=backend-builder /app/athome /usr/local/bin/
COPY --from=backend-builder /app/public /usr/local/bin/public

# Container metadata following OCI standards
LABEL org.opencontainers.image.title="AtHome"
LABEL org.opencontainers.image.description="Enhanced Selfhosted Bluesky Profile Interface"
LABEL org.opencontainers.image.source="https://github.com/mdaguete/athome"
LABEL org.opencontainers.image.licenses="MIT"
LABEL org.opencontainers.image.vendor="mdaguete"
LABEL org.opencontainers.image.version="1.0.0"

# Configuration
EXPOSE 8200
ENV ATHOME_BIND=:8200 \
    ATHOME_APPVIEW=https://api.bsky.app \
    ATHOME_VALID_HANDLES="" \
    ATHOME_PDS="" \
    ATHOME_PDS_HANDLE="" \
    ATHOME_PDS_PASSWORD=""

WORKDIR /usr/local/bin

# Use unprivileged user (provided by distroless)
USER nonroot

ENTRYPOINT ["/usr/local/bin/athome"]
