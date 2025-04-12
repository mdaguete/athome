package main

import (
	"context"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/labstack/echo/v4"
)

// Server represents the main application server
type Server struct {
	e               *echo.Echo
	xrpcc           *xrpc.Client
	dir             identity.Directory
	validHandles    []string
	auth            *AuthConfig
	authMutex       sync.RWMutex       // Protects auth token refresh operations
	refreshCancel   context.CancelFunc // For cancelling background token refresh
	enablePortfolio bool               // Flag to enable/disable portfolio feature
}

// AuthConfig manages PDS authentication and token refresh
// While xrpc.AuthInfo holds the current token for requests,
// AuthConfig maintains the credentials and refresh timing
type AuthConfig struct {
	// PDS server URL
	PDS string `json:"pds"`
	// User handle for authentication
	Handle string `json:"handle"`
	// User password for authentication
	Password string `json:"password"`
	// Current access token (managed by refreshAuth)
	Token string `json:"token,omitempty"`
	// Refresh token for session renewal
	RefreshToken string `json:"refresh_token,omitempty"`
	// Time when token should be refreshed
	RefreshAt time.Time `json:"refresh_at,omitempty"`
}

// GenericStatus represents a basic status response
type GenericStatus struct {
	Status string `json:"status"`
	Daemon string `json:"daemon"`
}

// PortfolioConfig represents the portfolio feature configuration
type PortfolioConfig struct {
	Enabled bool `json:"enabled"`
}

// PortfolioResponse represents the portfolio data response
type PortfolioResponse struct {
	Items []PortfolioItem `json:"items"`
}

// PortfolioItem represents a single portfolio item
type PortfolioItem struct {
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Link        string    `json:"link,omitempty"`
	Image       string    `json:"image,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}
