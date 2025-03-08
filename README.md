# AtHome: Enhanced Selfhosted Bluesky Profile Interface 

> This project is a fork and enhancement of the original [athome](https://github.com/bluesky-social/indigo/tree/main/cmd/athome) project by the Bluesky team.

## Overview

AtHome is an enhanced web interface for Bluesky profiles that provides a modern, responsive UI with features like:

- 🖼️ Responsive profile display with banner and avatar support
- 📱 Mobile-friendly design
- ⚡ Infinite scroll feed loading
- 🧵 Thread view support
- 🕒 Relative timestamps for posts
- 🎨 Modern, clean aesthetic

## Features

- **Profile Display**
  - Only list post whose author is the same as the handle, no reposts
  - Responsive layout adapting to different screen sizes
  - Links to original Bluesky profile


## Quick Start

### Using Docker/Podman

```bash
# Pull and run the container
podman run -d \
  -p 8200:8200 \
  -e ATHOME_BIND=:8200 \
  -e ATHOME_APPVIEW=https://api.bsky.app \
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

Environment variables:
- `ATHOME_BIND`: Server bind address (default: `:8200`)
- `ATHOME_APPVIEW`: Bluesky API host (default: `https://api.bsky.app`)
- `ATHOME_VALID_HANDLES`: Comma-separated list of allowed handles

## Deployment

### Using GitHub Actions

The project includes GitHub Actions workflows for:
- Automated container builds
- Multi-architecture support (amd64, arm64)
- Automated publishing to GitHub Container Registry
- Version tagging

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

## Original Project

This project builds upon the original [athome](https://github.com/bluesky-social/indigo/tree/main/cmd/athome) project by the Bluesky team. The original concept demonstrated a simple way to serve public Bluesky profiles. This enhanced version adds modern UI features while maintaining the core functionality adding some new features.

## License

This project maintains the same license as the original athome project from the Bluesky team.
