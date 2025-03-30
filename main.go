// Package main implements an enhanced web interface for Bluesky profiles
// supporting both AppView and PDS authentication modes.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/util"
	"github.com/bluesky-social/indigo/xrpc"
)

// defaultDirectory implements the identity.Directory interface by wrapping
// the default Bluesky directory service. It provides handle resolution and
// DID lookup capabilities.
type defaultDirectory struct {
	dir identity.Directory
}

// LookupHandle resolves a Bluesky handle to its corresponding identity.
// This is used to convert user handles to DIDs for API operations.
func (d *defaultDirectory) LookupHandle(ctx context.Context, handle syntax.Handle) (*identity.Identity, error) {
	return d.dir.LookupHandle(ctx, handle)
}

// Lookup resolves an AT identifier (handle or DID) to its corresponding identity.
func (d *defaultDirectory) Lookup(ctx context.Context, did syntax.AtIdentifier) (*identity.Identity, error) {
	return d.dir.Lookup(ctx, did)
}

// LookupDID resolves a DID to its corresponding identity.
func (d *defaultDirectory) LookupDID(ctx context.Context, did syntax.DID) (*identity.Identity, error) {
	return d.dir.LookupDID(ctx, did)
}

// Purge removes an identity from the directory cache.
func (d *defaultDirectory) Purge(ctx context.Context, did syntax.AtIdentifier) error {
	return d.dir.Purge(ctx, did)
}

// getEnvOrFlag retrieves a configuration value from either an environment variable
// or a command-line flag, prioritizing the environment variable if present.
//
// Parameters:
//   - envKey: The environment variable name
//   - flagValue: The command-line flag value
//
// Returns the environment variable value if set, otherwise the flag value.
func getEnvOrFlag(envKey, flagValue string) string {
	if env := os.Getenv(envKey); env != "" {
		return env
	}
	return flagValue
}

// getEnvListOrFlag retrieves a comma-separated list from either an environment variable
// or a command-line flag, splitting it into a slice of strings.
//
// Parameters:
//   - envKey: The environment variable name
//   - flagValue: The command-line flag value
//
// Returns a slice of strings, or nil if both sources are empty.
func getEnvListOrFlag(envKey string, flagValue string) []string {
	if env := os.Getenv(envKey); env != "" {
		return strings.Split(env, ",")
	}
	if flagValue == "" {
		return nil
	}
	return strings.Split(flagValue, ",")
}

// isValidHandle checks if a given handle is in the list of valid handles.
// If the validHandles list is empty, all handles are considered valid.
//
// Parameters:
//   - handle: The handle to validate
//   - validHandles: List of allowed handles
//
// Returns true if the handle is valid, false otherwise.
func isValidHandle(handle string, validHandles []string) bool {
	if len(validHandles) == 0 {
		return true
	}
	for _, h := range validHandles {
		if h == handle {
			return true
		}
	}
	return false
}

// Run initializes and starts the server with the provided configuration.
// It handles server setup and lifecycle management.
//
// Parameters:
//   - ctx: Context for lifecycle management
//   - bindAddr: Server bind address
//   - xrpcc: XRPC client for API communication
//   - dir: Identity directory service
//   - validHandles: List of allowed handles
//   - auth: Optional PDS authentication configuration
//
// Returns an error if server setup or operation fails.
func Run(ctx context.Context, bindAddr string, xrpcc *xrpc.Client, dir identity.Directory, validHandles []string, auth *AuthConfig) error {
	// Create and set up server
	srv, err := setupServer(bindAddr, xrpcc, dir, validHandles, auth)
	if err != nil {
		return fmt.Errorf("failed to set up server: %w", err)
	}

	// Start server and handle graceful shutdown
	return startServer(ctx, srv, bindAddr)
}

// main is the entry point of the application. It handles configuration loading,
// server setup, and graceful shutdown.
func main() {
	var bindAddr string
	var appviewHost string
	var validHandles string
	var pdsHost string
	var pdsHandle string
	var pdsPassword string

	// Parse command line flags
	flag.StringVar(&bindAddr, "bind", ":8200", "address to bind server to")
	flag.StringVar(&appviewHost, "appview", "https://api.bsky.app", "appview host to connect to")
	flag.StringVar(&validHandles, "valid-handles", "", "comma-separated list of valid handles")
	flag.StringVar(&pdsHost, "pds", "", "PDS host to connect to")
	flag.StringVar(&pdsHandle, "pds-handle", "", "handle to authenticate with PDS")
	flag.StringVar(&pdsPassword, "pds-password", "", "password to authenticate with PDS")
	flag.Parse()

	// Override flags with environment variables if present
	bindAddr = getEnvOrFlag("ATHOME_BIND", bindAddr)
	appviewHost = getEnvOrFlag("ATHOME_APPVIEW", appviewHost)
	validHandlesList := getEnvListOrFlag("ATHOME_VALID_HANDLES", validHandles)
	pdsHost = getEnvOrFlag("ATHOME_PDS", pdsHost)
	pdsHandle = getEnvOrFlag("ATHOME_PDS_HANDLE", pdsHandle)
	pdsPassword = getEnvOrFlag("ATHOME_PDS_PASSWORD", pdsPassword)

	// Set up logging
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// Validate configuration exclusivity
	isPDSConfigured := pdsHost != ""
	isAppViewConfigured := appviewHost != "https://api.bsky.app" // Check if non-default
	if isPDSConfigured && isAppViewConfigured {
		slog.Error("configuration error: cannot use both PDS and AppView configurations")
		os.Exit(1)
	}

	// Create XRPC client based on configuration
	var xrpcc *xrpc.Client
	var auth *AuthConfig

	if isPDSConfigured {
		if pdsHandle == "" || pdsPassword == "" {
			slog.Error("PDS host specified but missing handle or password")
			os.Exit(1)
		}

		// When using PDS, create both XRPC client and auth config
		xrpcc = &xrpc.Client{
			Client: util.RobustHTTPClient(),
			Host:   pdsHost,
		}

		// Create auth config for token management
		auth = &AuthConfig{
			PDS:      pdsHost,
			Handle:   pdsHandle,
			Password: pdsPassword,
		}

		slog.Info("using PDS configuration", "host", pdsHost)
	} else {
		// When using AppView, only create XRPC client
		xrpcc = &xrpc.Client{
			Client: util.RobustHTTPClient(),
			Host:   appviewHost,
		}

		slog.Info("using AppView configuration", "host", appviewHost)
	}

	// Set up context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		<-quit
		slog.Info("shutting down server...")
		cancel()
	}()

	// Create directory service wrapper
	dir := &defaultDirectory{
		dir: identity.DefaultDirectory(),
	}

	// Run server
	slog.Info("starting server",
		"bind", bindAddr,
		"host", xrpcc.Host,
		"auth_enabled", auth != nil,
	)

	if err := Run(ctx, bindAddr, xrpcc, dir, validHandlesList, auth); err != nil {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}
