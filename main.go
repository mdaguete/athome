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

type defaultDirectory struct {
	dir identity.Directory
}

func (d *defaultDirectory) LookupHandle(ctx context.Context, handle syntax.Handle) (*identity.Identity, error) {
	return d.dir.LookupHandle(ctx, handle)
}

func (d *defaultDirectory) Lookup(ctx context.Context, did syntax.AtIdentifier) (*identity.Identity, error) {
	return d.dir.Lookup(ctx, did)
}

func (d *defaultDirectory) LookupDID(ctx context.Context, did syntax.DID) (*identity.Identity, error) {
	return d.dir.LookupDID(ctx, did)
}

func (d *defaultDirectory) Purge(ctx context.Context, did syntax.AtIdentifier) error {
	return d.dir.Purge(ctx, did)
}

func getEnvOrFlag(envKey, flagValue string) string {
	if env := os.Getenv(envKey); env != "" {
		return env
	}
	return flagValue
}

func getEnvListOrFlag(envKey string, flagValue string) []string {
	if env := os.Getenv(envKey); env != "" {
		return strings.Split(env, ",")
	}
	if flagValue == "" {
		return nil
	}
	return strings.Split(flagValue, ",")
}

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

func Run(ctx context.Context, bindAddr string, xrpcc *xrpc.Client, dir identity.Directory, validHandles []string) error {
	// Create and set up server
	srv, err := setupServer(bindAddr, xrpcc, dir, validHandles)
	if err != nil {
		return fmt.Errorf("failed to set up server: %w", err)
	}

	// Start server and handle graceful shutdown
	return startServer(ctx, srv, bindAddr)
}

func main() {
	var bindAddr string
	var appviewHost string
	var validHandles string
	
	flag.StringVar(&bindAddr, "bind", ":8200", "address to bind server to")
	flag.StringVar(&appviewHost, "appview", "https://api.bsky.app", "appview host to connect to")
	flag.StringVar(&validHandles, "valid-handles", "", "comma-separated list of valid handles")
	flag.Parse()

	// Override flags with environment variables if present
	bindAddr = getEnvOrFlag("ATHOME_BIND", bindAddr)
	appviewHost = getEnvOrFlag("ATHOME_APPVIEW", appviewHost)
	validHandlesList := getEnvListOrFlag("ATHOME_VALID_HANDLES", validHandles)

	// Set up logging
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// Create XRPC client
	xrpcc := &xrpc.Client{
		Client: util.RobustHTTPClient(),
		Host:   appviewHost,
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
	slog.Info("starting server", "bind", bindAddr)
	if err := Run(ctx, bindAddr, xrpcc, dir, validHandlesList); err != nil {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}
