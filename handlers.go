package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/labstack/echo/v4"
)

// HandleHealthCheck responds to health check requests with a simple status message.
// This endpoint is used by monitoring systems to verify the service is running.
//
// Returns:
//   - 200 OK with GenericStatus if the service is healthy
func (srv *Server) HandleHealthCheck(c echo.Context) error {
	return c.JSON(200, GenericStatus{Status: "ok", Daemon: "athome"})
}

// validateHandle checks if the handle is in the allowed list of handles.
// If no handles are configured (empty list), all handles are allowed.
//
// Parameters:
//   - handle: The handle to validate
//
// Returns:
//   - nil if the handle is valid
//   - error if the handle is not in the allowed list
func (srv *Server) validateHandle(handle string) error {
	if len(srv.validHandles) == 0 {
		return nil
	}
	for _, h := range srv.validHandles {
		if h == handle {
			return nil
		}
	}
	return fmt.Errorf("handle %s is not in the allowed list", handle)
}

// getHandleFromRequest extracts the handle from either the URL parameter
// or the request hostname. This allows for both explicit handle parameters
// and hostname-based handle resolution.
//
// Parameters:
//   - c: The Echo context containing the request
//
// Returns:
//   - The extracted handle string
func getHandleFromRequest(c echo.Context) string {
	// First try to get handle from URL parameter
	handle := c.Param("handle")
	if handle != "" {
		return handle
	}

	// If no handle provided, use hostname
	host := c.Request().Host
	// Remove port if present
	if idx := strings.Index(host, ":"); idx != -1 {
		host = host[:idx]
	}
	return host
}

// validateAndGetDID validates a handle and resolves it to a DID.
// This is a common operation used by multiple handlers to ensure
// the handle is valid and get its corresponding DID for API operations.
//
// Parameters:
//   - c: The Echo context
//   - handle: The handle to validate and resolve
//
// Returns:
//   - The resolved DID string
//   - error if validation fails or DID resolution fails
func (srv *Server) validateAndGetDID(c echo.Context, handle string) (string, error) {
	if handle == "" {
		return "", echo.NewHTTPError(http.StatusBadRequest, "handle is required")
	}

	// Parse handle to ensure it's valid
	h, err := syntax.ParseHandle(handle)
	if err != nil {
		slog.Error("invalid handle format", "error", err)
		return "", echo.NewHTTPError(http.StatusBadRequest, "invalid handle format")
	}

	// Validate handle against allowed list
	if err := srv.validateHandle(handle); err != nil {
		slog.Error("handle not allowed", "error", err)
		return "", echo.NewHTTPError(http.StatusForbidden, err.Error())
	}

	// Look up the handle to get the DID
	ident, err := srv.dir.LookupHandle(c.Request().Context(), h)
	if err != nil {
		slog.Error("failed to lookup handle", "error", err)
		return "", echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return ident.DID.String(), nil
}

// handleGetProfile handles requests for user profile information.
// It validates the handle, resolves it to a DID, and fetches the
// profile data from the Bluesky API.
//
// URL Parameters:
//   - handle: Optional handle parameter (falls back to hostname)
//
// Returns:
//   - 200 OK with profile data
//   - 400 Bad Request if handle is invalid
//   - 403 Forbidden if handle is not allowed
//   - 500 Internal Server Error if profile fetch fails
func (srv *Server) handleGetProfile(c echo.Context) error {
	handle := getHandleFromRequest(c)
	did, err := srv.validateAndGetDID(c, handle)
	if err != nil {
		return err
	}

	// Get profile using DID
	profile, err := bsky.ActorGetProfile(c.Request().Context(), srv.xrpcc, did)
	if err != nil {
		slog.Error("failed to fetch profile", "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	// Transform profile data using ActorDefs_ProfileViewDetailed
	response := map[string]interface{}{
		"did":            profile.Did,
		"handle":         profile.Handle,
		"displayName":    profile.DisplayName,
		"description":    profile.Description,
		"avatar":         profile.Avatar,
		"banner":         profile.Banner,
		"followsCount":   profile.FollowsCount,
		"followersCount": profile.FollowersCount,
		"postsCount":     profile.PostsCount,
		"indexedAt":      profile.IndexedAt,
	}

	return c.JSON(http.StatusOK, response)
}

// handleGetFeed handles requests for a user's feed.
// It validates the handle, resolves it to a DID, and fetches
// the feed data from the Bluesky API. The feed is filtered to
// only include posts by the specified handle.
//
// URL Parameters:
//   - handle: Optional handle parameter (falls back to hostname)
//
// Query Parameters:
//   - cursor: Pagination cursor for fetching more posts
//
// Returns:
//   - 200 OK with feed data
//   - 400 Bad Request if handle is invalid
//   - 403 Forbidden if handle is not allowed
//   - 500 Internal Server Error if feed fetch fails
func (srv *Server) handleGetFeed(c echo.Context) error {
	handle := getHandleFromRequest(c)
	did, err := srv.validateAndGetDID(c, handle)
	if err != nil {
		return err
	}

	cursor := c.QueryParam("cursor")
	slog.Info("fetching feed", "did", did, "cursor", cursor)

	// Get feed using DID
	feed, err := bsky.FeedGetAuthorFeed(c.Request().Context(), srv.xrpcc, did, cursor, "posts_no_replies", false, 20)
	if err != nil {
		slog.Error("failed to fetch feed", "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	// Ensure feed is not nil before returning
	if feed == nil || feed.Feed == nil {
		slog.Error("feed data is nil")
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to fetch feed data")
	}

	// Filter feed whose author is the handle
	filteredFeed := []*bsky.FeedDefs_FeedViewPost{}
	for _, post := range feed.Feed {
		if post.Post.Author.Handle == handle {
			filteredFeed = append(filteredFeed, post)
		}
	}

	// Transform feed data using FeedDefs_FeedViewPost
	response := map[string]interface{}{
		"cursor": feed.Cursor,
		"feed":   filteredFeed,
	}

	return c.JSON(http.StatusOK, response)
}

// handleGetPost handles requests for a specific post and its thread.
// It accepts an AT-URI and fetches the post and surrounding thread
// context from the Bluesky API.
//
// URL Parameters:
//   - *: The AT-URI of the post (with or without at:// prefix)
//
// Returns:
//   - 200 OK with post and thread data
//   - 400 Bad Request if URI is invalid
//   - 500 Internal Server Error if post fetch fails
func (srv *Server) handleGetPost(c echo.Context) error {
	// Get full URI path from wildcard parameter
	uri := c.Param("*")
	if uri == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "uri is required")
	}

	// Add at:// prefix if not present
	if !strings.HasPrefix(uri, "at://") {
		uri = "at://" + uri
	}

	slog.Info("fetching post", "uri", uri)

	// Parse AT-URI
	atUri, err := syntax.ParseATURI(uri)
	if err != nil {
		slog.Error("invalid uri format", "error", err)
		return echo.NewHTTPError(http.StatusBadRequest, "invalid uri format")
	}

	// Get thread with depth 8 for context
	thread, err := bsky.FeedGetPostThread(c.Request().Context(), srv.xrpcc, 8, 0, atUri.String())
	if err != nil {
		slog.Error("failed to fetch post", "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, thread)
}

// handleIndex serves the main SPA (Single Page Application) HTML.
// It injects necessary data attributes and security nonces into
// the HTML before serving it.
//
// Returns:
//   - 200 OK with the modified index.html content
//   - 500 Internal Server Error if index.html cannot be read
func (srv *Server) handleIndex(c echo.Context) error {
	nonce := c.Get("nonce").(string)

	// Read the Vite-built index.html
	content, err := os.ReadFile("public/index.html")
	if err != nil {
		slog.Error("failed to read index.html", "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to read index.html")
	}

	defaultHandle := getHandleFromRequest(c)

	// Add nonce to all script tags
	doc := string(content)
	scriptPattern := `<script`
	nonceAttr := ` nonce="` + nonce + `"`

	modifiedContent := strings.ReplaceAll(doc, scriptPattern, scriptPattern+nonceAttr)

	// Add the default handle as a data attribute to html tag
	modifiedContent = strings.Replace(
		modifiedContent,
		`<html lang="en"`,
		`<html lang="en" data-default-handle="`+defaultHandle+`"`,
		1,
	)

	// Add the index.html title using the handle
	modifiedContent = strings.ReplaceAll(modifiedContent, "<title>AtHome</title>", "<title>@"+defaultHandle+"</title>")

	// Set proper content type
	c.Response().Header().Set(echo.HeaderContentType, echo.MIMETextHTMLCharsetUTF8)
	return c.HTMLBlob(http.StatusOK, []byte(modifiedContent))
}

// refreshAuth handles PDS authentication token refresh.
// It checks if the current token needs refresh and obtains a new one
// if necessary. This is used by the auth middleware when PDS mode is enabled.
// The function uses a read-write mutex to prevent concurrent token refreshes
// while allowing multiple requests to use the same token.
//
// Parameters:
//   - c: The Echo context
//
// Returns:
//   - nil if refresh successful or not needed
//   - error if refresh fails or no auth config is present
func (srv *Server) refreshAuth(c echo.Context) error {
	if srv.auth == nil {
		return fmt.Errorf("no auth configuration")
	}

	// First acquire a read lock to check if refresh is needed
	srv.authMutex.RLock()
	if !srv.auth.RefreshAt.IsZero() && time.Now().Before(srv.auth.RefreshAt) {
		srv.authMutex.RUnlock()
		return nil
	}
	srv.authMutex.RUnlock()

	// If we need to refresh, acquire write lock
	srv.authMutex.Lock()
	defer srv.authMutex.Unlock()

	// Double-check if refresh is still needed after acquiring write lock
	// This prevents multiple refreshes if another goroutine refreshed while we were waiting
	if !srv.auth.RefreshAt.IsZero() && time.Now().Before(srv.auth.RefreshAt) {
		return nil
	}

	// If we don't have a token yet, create a new session
	if srv.auth.Token == "" {
		session, err := atproto.ServerCreateSession(c.Request().Context(), srv.xrpcc, &atproto.ServerCreateSession_Input{
			Identifier: srv.auth.Handle,
			Password:   srv.auth.Password,
		})
		if err != nil {
			return fmt.Errorf("failed to create session: %w", err)
		}
		srv.auth.Token = session.AccessJwt
		srv.auth.RefreshAt = time.Now().Add(time.Hour * 23) // Refresh 1 hour before expiry
		srv.xrpcc.Auth = &xrpc.AuthInfo{AccessJwt: session.AccessJwt}
		return nil
	}

	// Refresh existing token
	// Note: In a production environment, you might want to implement token refresh
	// using the appropriate Bluesky API endpoint. For now, we just create a new session.
	session, err := atproto.ServerCreateSession(c.Request().Context(), srv.xrpcc, &atproto.ServerCreateSession_Input{
		Identifier: srv.auth.Handle,
		Password:   srv.auth.Password,
	})
	if err != nil {
		return fmt.Errorf("failed to refresh session: %w", err)
	}

	srv.auth.Token = session.AccessJwt
	srv.auth.RefreshAt = time.Now().Add(time.Hour * 23) // Refresh 1 hour before expiry
	srv.xrpcc.Auth = &xrpc.AuthInfo{AccessJwt: session.AccessJwt}
	return nil
}
