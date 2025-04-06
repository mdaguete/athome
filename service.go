package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// generateNonce creates a cryptographically secure random nonce for Content Security Policy.
// The nonce is a 16-byte random value encoded in base64, used to validate inline scripts.
// This helps prevent Cross-Site Scripting (XSS) attacks by ensuring only server-generated
// scripts with the correct nonce can execute.
func generateNonce() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

// setupServer initializes and configures the Echo web server with all necessary middleware,
// routes, and security settings.
//
// Parameters:
//   - bindAddr: The address and port to bind the server to
//   - xrpcClient: The XRPC client for Bluesky API communication
//   - dir: The identity directory service for handle resolution
//   - validHandles: List of allowed handles for access control
//   - authConfig: Optional PDS authentication configuration
//
// Returns:
//   - *Server: Configured server instance
//   - error: Any error encountered during setup
//
// Security features:
//   - Content Security Policy (CSP) with dynamic nonces
//   - XSS Protection
//   - Content Type verification
//   - Frame options control
//   - HSTS support
//   - Request size limits
//   - CORS configuration
func setupServer(bindAddr string, xrpcClient *xrpc.Client, dir identity.Directory, validHandles []string, authConfig *AuthConfig) (*Server, error) {
	e := echo.New()
	e.HideBanner = true

	// Set up security middleware with improved CSP
	e.Use(middleware.SecureWithConfig(middleware.SecureConfig{
		XSSProtection:      "1; mode=block",
		ContentTypeNosniff: "nosniff",
		XFrameOptions:      "SAMEORIGIN",
		HSTSMaxAge:         31536000,
		ContentSecurityPolicy: func() string {
			extraHost := ""
			if authConfig != nil && authConfig.PDS != "" {
				extraHost = authConfig.PDS
			}
			return fmt.Sprintf(`default-src 'self';
				script-src 'self' 'nonce-{nonce}';
				style-src 'self' 'unsafe-inline' https://fonts.googleapis.com;
				font-src 'self' https://fonts.gstatic.com;
				img-src 'self' data: https:;
				connect-src 'self' https://api.bsky.app %s;
				manifest-src 'self';
				worker-src 'self'`, extraHost)
		}(),
	}))

	// Add nonce middleware for CSP script validation
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			nonce := generateNonce()
			c.Set("nonce", nonce)
			// Update CSP header with actual nonce
			csp := c.Response().Header().Get("Content-Security-Policy")
			c.Response().Header().Set("Content-Security-Policy",
				strings.Replace(csp, "{nonce}", nonce, 1))
			return next(c)
		}
	})

	// Set up standard middleware stack
	e.Use(middleware.Logger())              // Request logging
	e.Use(middleware.Recover())             // Panic recovery
	e.Use(middleware.CORS())                // Cross-Origin Resource Sharing
	e.Use(middleware.BodyLimit("64M"))      // Request size limiting
	e.Use(middleware.RemoveTrailingSlash()) // URL normalization

	// Create server instance with dependencies
	srv := &Server{
		e:            e,
		xrpcc:        xrpcClient,
		dir:          dir,
		validHandles: validHandles,
		auth:         authConfig,
	}

	// Add server instance to context for middleware access
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set("server", srv)
			return next(c)
		}
	})

	// Configure authentication refresh middleware when using PDS
	if authConfig != nil {
		// Create a context for background refresh that will be cancelled when server stops
		refreshCtx, refreshCancel := context.WithCancel(context.Background())
		srv.refreshCancel = refreshCancel

		// Start background token refresh
		go func() {
			ticker := time.NewTicker(5 * time.Minute)
			defer ticker.Stop()

			for {
				select {
				case <-refreshCtx.Done():
					slog.Info("stopping background token refresh")
					return
				case <-ticker.C:
					// Check if we need to refresh
					srv.authMutex.RLock()
					needsRefresh := srv.auth.RefreshAt.IsZero() || time.Now().After(srv.auth.RefreshAt.Add(-10*time.Minute))
					srv.authMutex.RUnlock()

					if needsRefresh {
						// Create a new session directly
						session, err := atproto.ServerCreateSession(refreshCtx, srv.xrpcc, &atproto.ServerCreateSession_Input{
							Identifier: srv.auth.Handle,
							Password:   srv.auth.Password,
						})
						if err != nil {
							slog.Error("background token refresh failed", "error", err)
							continue
						}

						// Update token info under lock
						srv.authMutex.Lock()
						srv.auth.Token = session.AccessJwt
						srv.auth.RefreshAt = time.Now().Add(time.Hour * 23) // Refresh 1 hour before expiry
						srv.xrpcc.Auth = &xrpc.AuthInfo{AccessJwt: session.AccessJwt}
						srv.authMutex.Unlock()

						slog.Info("background token refresh successful")
					}
				}
			}
		}()

		// Also keep the request middleware for immediate refresh if needed
		e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error {
				srv := c.Get("server").(*Server)

				// Check if we need to refresh
				srv.authMutex.RLock()
				needsRefresh := srv.auth.RefreshAt.IsZero() || time.Now().After(srv.auth.RefreshAt.Add(-10*time.Minute))
				srv.authMutex.RUnlock()

				if needsRefresh {
					// Create a new session directly
					session, err := atproto.ServerCreateSession(c.Request().Context(), srv.xrpcc, &atproto.ServerCreateSession_Input{
						Identifier: srv.auth.Handle,
						Password:   srv.auth.Password,
					})
					if err != nil {
						slog.Error("failed to refresh auth", "error", err)
						return echo.NewHTTPError(http.StatusUnauthorized, "authentication failed")
					}

					// Update token info under lock
					srv.authMutex.Lock()
					srv.auth.Token = session.AccessJwt
					srv.auth.RefreshAt = time.Now().Add(time.Hour * 23) // Refresh 1 hour before expiry
					srv.xrpcc.Auth = &xrpc.AuthInfo{AccessJwt: session.AccessJwt}
					srv.authMutex.Unlock()
				}
				return next(c)
			}
		})
	}

	// Register API routes
	e.GET("/healthz", srv.HandleHealthCheck) // Health check endpoint

	// Group API routes under /api
	api := e.Group("/api")
	{
		// Handle-specific routes
		api.GET("/profile/:handle", srv.handleGetProfile) // Get profile by handle
		api.GET("/feed/:handle", srv.handleGetFeed)       // Get feed by handle
		api.GET("/post/*", srv.handleGetPost)             // Get post by AT-URI

		// Hostname-based routes (handle derived from hostname)
		api.GET("/profile", srv.handleGetProfile)
		api.GET("/feed", srv.handleGetFeed)
	}

	// SPA routes - serve index.html for client-side routing
	e.GET("/", srv.handleIndex)
	e.GET("/app", srv.handleIndex)
	e.GET("/app/*", srv.handleIndex)
	e.GET("/profile/*", srv.handleIndex)
	e.GET("/feed/*", srv.handleIndex)
	e.GET("/post/*", srv.handleIndex)

	// Static file serving
	e.Static("/assets", "public/assets") // Vite assets
	e.Static("/", "public")              // Root static files

	return srv, nil
}

// startServer launches the HTTP server and manages its lifecycle.
// It handles graceful shutdown on context cancellation and returns any errors
// encountered during startup or shutdown.
//
// Parameters:
//   - ctx: Context for lifecycle management
//   - srv: The configured server instance
//   - bindAddr: The address to bind the server to
//
// Returns:
//   - error: Any error encountered during server operation
//
// The server can be stopped by:
//   - Context cancellation (graceful shutdown)
//   - Server startup failure
//   - Shutdown errors
func startServer(ctx context.Context, srv *Server, bindAddr string) error {
	errChan := make(chan error, 1)

	// Start server in goroutine
	go func() {
		if err := srv.e.Start(bindAddr); err != nil && err != http.ErrServerClosed {
			errChan <- fmt.Errorf("failed to start server: %w", err)
		}
	}()

	// Wait for shutdown signal or error
	select {
	case <-ctx.Done():
		// Cancel background refresh if it's running
		if srv.refreshCancel != nil {
			srv.refreshCancel()
		}

		// Attempt graceful shutdown
		if err := srv.e.Shutdown(context.Background()); err != nil {
			return fmt.Errorf("failed to shutdown server: %w", err)
		}
		return nil
	case err := <-errChan:
		// Cancel background refresh on error
		if srv.refreshCancel != nil {
			srv.refreshCancel()
		}
		return err
	}
}
