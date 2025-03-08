package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

type Server struct {
	e            *echo.Echo
	xrpcc        *xrpc.Client
	dir          identity.Directory
	validHandles []string
}

// generateNonce creates a random nonce for CSP
func generateNonce() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

func setupServer(bindAddr string, xrpcc *xrpc.Client, dir identity.Directory, validHandles []string) (*Server, error) {
	e := echo.New()
	e.HideBanner = true

	// Set up security middleware with improved CSP
	e.Use(middleware.SecureWithConfig(middleware.SecureConfig{
		XSSProtection:      "1; mode=block",
		ContentTypeNosniff: "nosniff",
		XFrameOptions:      "SAMEORIGIN",
		HSTSMaxAge:         31536000,
		ContentSecurityPolicy: `default-src 'self'; 
			script-src 'self' 'nonce-{nonce}'; 
			style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; 
			font-src 'self' https://fonts.gstatic.com; 
			img-src 'self' data: https:; 
			connect-src 'self' https://api.bsky.app;
			manifest-src 'self';
			worker-src 'self'`,
	}))

	// Add nonce middleware
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

	// Set up other middleware
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.CORS())
	e.Use(middleware.BodyLimit("64M"))
	e.Use(middleware.RemoveTrailingSlash())

	// Create server instance
	srv := &Server{
		e:            e,
		xrpcc:        xrpcc,
		dir:          dir,
		validHandles: validHandles,
	}

	// Health check
	e.GET("/healthz", srv.HandleHealthCheck)

	// API routes
	api := e.Group("/api")
	{
		// Routes with handle parameter
		api.GET("/profile/:handle", srv.handleGetProfile)
		api.GET("/feed/:handle", srv.handleGetFeed)
		api.GET("/post/*", srv.handleGetPost)

		// Routes using hostname as handle
		api.GET("/profile", srv.handleGetProfile)
		api.GET("/feed", srv.handleGetFeed)
	}

	// Frontend routes - all routes should serve index.html for SPA
	e.GET("/", srv.handleIndex)
	e.GET("/app", srv.handleIndex)
	e.GET("/app/*", srv.handleIndex)
	e.GET("/profile/*", srv.handleIndex)
	e.GET("/feed/*", srv.handleIndex)
	e.GET("/post/*", srv.handleIndex)

	// Serve static files with proper paths
	e.Static("/assets", "public/assets") // For Vite's generated assets
	e.Static("/", "public")              // For other static files like favicon, etc.

	return srv, nil
}

func startServer(ctx context.Context, srv *Server, bindAddr string) error {
	errChan := make(chan error, 1)
	go func() {
		if err := srv.e.Start(bindAddr); err != nil && err != http.ErrServerClosed {
			errChan <- fmt.Errorf("failed to start server: %w", err)
		}
	}()

	select {
	case <-ctx.Done():
		if err := srv.e.Shutdown(context.Background()); err != nil {
			return fmt.Errorf("failed to shutdown server: %w", err)
		}
		return nil
	case err := <-errChan:
		return err
	}
}
