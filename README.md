# AtHome: Enhanced Selfhosted Bluesky Profile Interface

> This project is a fork and enhancement of the original [athome](https://github.com/bluesky-social/indigo/tree/main/cmd/athome) project by the Bluesky team.

## Overview

AtHome is an enhanced web interface for Bluesky profiles that provides a modern, responsive UI with features like:

- 🖼️ Responsive profile display with banner and avatar support
- 📱 Mobile-friendly design
- ⚡ Infinite scroll feed loading
- 🧵 Thread view support with depth up to 8 levels
- 🕒 Relative timestamps for posts
- 🎨 Modern, clean aesthetic with CSP security
- 🔐 Support for local PDS authentication with token refresh
- 🔍 Smart handle resolution from URL or hostname
- 🛡️ Enhanced security with CSP nonce and CORS support

## Features

- **Profile Display**
  - Clean profile view with avatar, banner, and stats
  - Display name, description, and follower counts
  - Posts filtered to show only author's content (no reposts)
  - Responsive layout adapting to different screen sizes
  - Direct links to original Bluesky profile

- **Feed Features**
  - Infinite scroll with cursor-based pagination
  - Post threading support up to 8 levels deep
  - Filtered feed showing only original posts (no reposts)
  - Smart post URI handling with at:// protocol support

- **PDS Support**
  - Connect to local or custom PDS instances
  - Automatic token management and refresh
  - Secure credential handling
  - Token refresh 1 hour before expiry
  - Graceful authentication error handling

- **Security Features**
  - Content Security Policy (CSP) with nonce support
  - XSS protection and frame options
  - CORS configuration for API access
  - Request size limits (64MB)
  - HSTS support with 1-year max age

## Quick Start

### Using Docker/Podman

```bash
# Using AppView (public Bluesky API)
podman run -d \
  -p 8200:8200 \
  -e ATHOME_BIND=:8200 \
  -e ATHOME_APPVIEW=https://api.bsky.app \
  -e ATHOME_VALID_HANDLES="your.handle" \
  ghcr.io/your-username/athome:latest

# Using PDS (local or custom PDS)
podman run -d \
  -p 8200:8200 \
  -e ATHOME_BIND=:8200 \
  -e ATHOME_PDS="https://your-pds.example.com" \
  -e ATHOME_PDS_HANDLE="your.handle" \
  -e ATHOME_PDS_PASSWORD="your-password" \
  -e ATHOME_VALID_HANDLES="your.handle" \
  ghcr.io/your-username/athome:latest
```

### Building Locally

1. **Prerequisites**
   - Go 1.21+
   - Node.js 20+
   - Make

2. **Build and Run**
   ```bash
   # Install dependencies
   make deps

   # Build frontend and backend
   make build

   # Run the application
   make run
   ```

## Development

```bash
# Run in development mode (hot reload)
make dev

# Build container
make container

# Run container
make container-run
```

## Configuration

The application can be configured to use either the public Bluesky AppView API or a Personal Data Server (PDS). These configurations are mutually exclusive - you should use either AppView OR PDS configuration, not both.

### AppView Configuration (Public Bluesky API)
Environment variables:
- `ATHOME_BIND`: Server bind address (default: `:8200`)
- `ATHOME_APPVIEW`: Bluesky API host (default: `https://api.bsky.app`)
- `ATHOME_VALID_HANDLES`: Comma-separated list of allowed handles

Command line flags:
- `--bind`: Server bind address (default: `:8200`)
- `--appview`: Bluesky API host (default: `https://api.bsky.app`)
- `--valid-handles`: Comma-separated list of allowed handles

### PDS Configuration (Local or Custom PDS)
Environment variables:
- `ATHOME_BIND`: Server bind address (default: `:8200`)
- `ATHOME_PDS`: PDS host to connect to
- `ATHOME_PDS_HANDLE`: Handle to authenticate with PDS
- `ATHOME_PDS_PASSWORD`: Password to authenticate with PDS
- `ATHOME_VALID_HANDLES`: Comma-separated list of allowed handles

Command line flags:
- `--bind`: Server bind address (default: `:8200`)
- `--pds`: PDS host to connect to
- `--pds-handle`: Handle to authenticate with PDS
- `--pds-password`: Password to authenticate with PDS
- `--valid-handles`: Comma-separated list of allowed handles

## API Endpoints

- `/healthz` - Health check endpoint
- `/api/profile/:handle` - Get profile by handle
- `/api/feed/:handle` - Get user feed by handle
- `/api/post/*` - Get post and thread by AT-URI
- `/api/profile` - Get profile using hostname as handle
- `/api/feed` - Get feed using hostname as handle

## Security

The application implements several security measures:

- Content Security Policy (CSP) with dynamic nonces
- XSS Protection headers
- Content Type nosniff
- X-Frame-Options for same origin
- HSTS with 1-year max age
- Request body size limits
- CORS configuration
- Secure token management for PDS authentication

## Deployment

### Using GitHub Actions

The project includes GitHub Actions workflows for:
- Automated container builds
- Multi-architecture support (amd64, arm64)
- Automated publishing to GitHub Container Registry
- Version tagging
- Security scanning

### Manual Deployment

You can deploy behind a reverse proxy like Caddy or Nginx. Example Caddy configuration:

```caddy
{
  on_demand_tls {
    interval 1h
    burst 8
  }
}

:443 {
  reverse_proxy localhost:8200
  tls {
    on_demand
  }
}
```

## Container Details

The container image is built using a multi-stage process:
1. Frontend build using Node.js 20 Alpine
2. Backend build using Go 1.23 Alpine
3. Final stage using distroless static Debian 12 for minimal attack surface

The container runs as a non-root user and includes OCI standard labels for metadata.

## Original Project

This project builds upon the original [athome](https://github.com/bluesky-social/indigo/tree/main/cmd/athome) project by the Bluesky team. While maintaining the core functionality, this enhanced version adds modern UI features, improved security, and robust PDS authentication support.

## License

This project maintains the same license as the original athome project from the Bluesky team.
