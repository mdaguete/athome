# Frontend build stage
FROM docker.io/library/node:20-alpine AS frontend-builder

WORKDIR /app/frontend
COPY frontend/package*.json ./
RUN npm ci

COPY frontend/ ./
RUN npm run build

# Go builder stage
FROM docker.io/library/golang:1.21-alpine AS backend-builder

# Install build dependencies
RUN apk add --no-cache git build-base

WORKDIR /app

# Cache dependencies layer
COPY go.mod go.sum ./
RUN go mod download

# Build layer
COPY . .

# Create public directory and copy frontend build
RUN mkdir -p public && \
    cp -r frontend/dist/* public/

# Build for multiple architectures
RUN CGO_ENABLED=0 go build -ldflags="-w -s" -o athome .

# Final stage - using distroless for minimal attack surface
FROM gcr.io/distroless/static-debian12:nonroot

# Copy binary and static files
COPY --from=backend-builder /app/athome /usr/local/bin/
COPY --from=backend-builder /app/public /usr/local/bin/public

# Container metadata
LABEL org.opencontainers.image.title="AtHome"
LABEL org.opencontainers.image.description="Enhanced Bluesky web interface with modern UI features"
LABEL org.opencontainers.image.source="https://github.com/mdaguete/athome"
LABEL org.opencontainers.image.licenses="MIT"

# Configuration
EXPOSE 8200
ENV ATHOME_BIND=:8200 \
    ATHOME_APPVIEW=https://api.bsky.app \
    ATHOME_VALID_HANDLES=""

WORKDIR /usr/local/bin

# Use unprivileged user (provided by distroless)
USER nonroot

ENTRYPOINT ["/usr/local/bin/athome"]
