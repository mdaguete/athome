package main

import (
	"encoding/base64"
	"encoding/json"
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

// extractTokenExpiry extracts the expiry time from a JWT token.
// JWT tokens are structured as three base64-encoded segments separated by dots.
// The middle segment contains the claims, including the "exp" claim which is the expiry time.
// Returns a zero time if the expiry time cannot be extracted.
func extractTokenExpiry(token string) time.Time {
	// Split the token into its three parts
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		slog.Warn("invalid JWT token format")
		return time.Time{}
	}

	// Decode the claims part (the middle part)
	claimsPart := parts[1]

	// Add padding if needed
	if len(claimsPart)%4 != 0 {
		claimsPart += strings.Repeat("=", 4-len(claimsPart)%4)
	}

	// Decode the base64 string
	claimsBytes, err := base64.URLEncoding.DecodeString(claimsPart)
	if err != nil {
		slog.Warn("failed to decode JWT claims", "error", err)
		return time.Time{}
	}

	// Parse the JSON
	var claims map[string]interface{}
	if err := json.Unmarshal(claimsBytes, &claims); err != nil {
		slog.Warn("failed to parse JWT claims", "error", err)
		return time.Time{}
	}

	// Extract the exp claim
	expClaim, ok := claims["exp"]
	if !ok {
		slog.Warn("JWT token does not contain exp claim")
		return time.Time{}
	}

	// Convert the exp claim to a time.Time
	var expTime time.Time
	switch exp := expClaim.(type) {
	case float64:
		// exp is in seconds since epoch
		expTime = time.Unix(int64(exp), 0)
	case int64:
		expTime = time.Unix(exp, 0)
	case json.Number:
		expInt, err := exp.Int64()
		if err != nil {
			slog.Warn("failed to convert exp claim to int64", "error", err)
			return time.Time{}
		}
		expTime = time.Unix(expInt, 0)
	default:
		slog.Warn("exp claim has unexpected type", "type", fmt.Sprintf("%T", expClaim))
		return time.Time{}
	}

	return expTime
}

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

// ensureValidToken ensures that the token is valid before making API requests.
// It forces a token refresh if the token is expired or about to expire.
func (srv *Server) ensureValidToken(c echo.Context) error {
	// Always force a token refresh before making API requests
	// This is a more aggressive approach to ensure we always have a valid token
	slog.Info("forcing token refresh before API request")
	return srv.refreshAuth(c)
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

	// Ensure we have a valid token before making the API request
	if err := srv.ensureValidToken(c); err != nil {
		slog.Error("failed to ensure valid token", "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Authentication error: "+err.Error())
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

	// Ensure we have a valid token before making the API request
	if err := srv.ensureValidToken(c); err != nil {
		slog.Error("failed to ensure valid token", "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Authentication error: "+err.Error())
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

	// Ensure we have a valid token before making the API request
	if err := srv.ensureValidToken(c); err != nil {
		slog.Error("failed to ensure valid token", "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Authentication error: "+err.Error())
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

	// Log that we're checking token expiry
	slog.Info("checking if token needs refresh")

	// First acquire a read lock to check if refresh is needed
	srv.authMutex.RLock()
	tokenExpired := srv.auth.RefreshAt.IsZero() || time.Now().After(srv.auth.RefreshAt.Add(-30*time.Minute))
	slog.Info("token expiry check result",
		"token_expired", tokenExpired,
		"refresh_at", srv.auth.RefreshAt,
		"now", time.Now(),
		"time_until_refresh", srv.auth.RefreshAt.Sub(time.Now()))

	if !tokenExpired {
		slog.Info("token is still valid, no refresh needed")
		srv.authMutex.RUnlock()
		return nil
	}
	srv.authMutex.RUnlock()

	// If we need to refresh, acquire write lock
	srv.authMutex.Lock()
	defer srv.authMutex.Unlock()

	// Double-check if refresh is still needed after acquiring write lock
	// This prevents multiple refreshes if another goroutine refreshed while we were waiting
	tokenExpired = srv.auth.RefreshAt.IsZero() || time.Now().After(srv.auth.RefreshAt.Add(-30*time.Minute))
	if !tokenExpired {
		return nil
	}

	// Log that we're refreshing the token
	slog.Info("token needs refresh",
		"refresh_at", srv.auth.RefreshAt,
		"now", time.Now(),
		"time_until_refresh", srv.auth.RefreshAt.Sub(time.Now()))

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
		srv.auth.RefreshToken = session.RefreshJwt

		// Extract expiry time from the token if possible
		expiry := extractTokenExpiry(session.AccessJwt)
		if expiry.IsZero() {
			// If we can't extract the expiry time, use a conservative default
			srv.auth.RefreshAt = time.Now().Add(time.Hour * 23) // Refresh 1 hour before assumed 24-hour expiry
			slog.Warn("could not extract token expiry time, using default refresh time")
		} else {
			// Set refresh time to 30 minutes before expiry
			srv.auth.RefreshAt = expiry.Add(-30 * time.Minute)
			slog.Info("extracted token expiry time", "expiry", expiry)
		}

		srv.xrpcc.Auth = &xrpc.AuthInfo{AccessJwt: session.AccessJwt}
		slog.Info("initial session created successfully",
			"refresh_at", srv.auth.RefreshAt,
			"refresh_in", srv.auth.RefreshAt.Sub(time.Now()),
			"token_expiry", expiry)
		return nil
	}

	// Try to refresh the token using the refresh token
	if srv.auth.RefreshToken != "" {
		// Use the refresh token to get a new access token
		slog.Info("refreshing session using refresh token")

		// Set the refresh token in the Auth field of the XRPC client
		tempAuth := srv.xrpcc.Auth
		srv.xrpcc.Auth = &xrpc.AuthInfo{RefreshJwt: srv.auth.RefreshToken}

		// Call ServerRefreshSession with the refresh token in the Auth field
		refreshedSession, err := atproto.ServerRefreshSession(c.Request().Context(), srv.xrpcc)

		// Restore the original Auth field
		srv.xrpcc.Auth = tempAuth

		if err == nil {
			// Successfully refreshed the token
			srv.auth.Token = refreshedSession.AccessJwt
			srv.auth.RefreshToken = refreshedSession.RefreshJwt

			// Extract expiry time from the token if possible
			// The token is a JWT which contains an "exp" claim
			// We'll set the refresh time to 30 minutes before the token expires
			expiry := extractTokenExpiry(refreshedSession.AccessJwt)
			if expiry.IsZero() {
				// If we can't extract the expiry time, use a conservative default
				srv.auth.RefreshAt = time.Now().Add(time.Hour * 23) // Refresh 1 hour before assumed 24-hour expiry
				slog.Warn("could not extract token expiry time, using default refresh time")
			} else {
				// Set refresh time to 30 minutes before expiry
				srv.auth.RefreshAt = expiry.Add(-30 * time.Minute)
				slog.Info("extracted token expiry time", "expiry", expiry)
			}

			srv.xrpcc.Auth = &xrpc.AuthInfo{AccessJwt: refreshedSession.AccessJwt}
			slog.Info("session refreshed successfully using refresh token",
				"refresh_at", srv.auth.RefreshAt,
				"refresh_in", srv.auth.RefreshAt.Sub(time.Now()),
				"token_expiry", expiry)
			return nil
		}

		// If refresh token is invalid or expired, log the error and fall back to creating a new session
		slog.Error("failed to refresh session using refresh token, falling back to creating new session", "error", err)
	}

	// Fall back to creating a new session if refresh token is missing or invalid
	slog.Info("creating new session")
	session, err := atproto.ServerCreateSession(c.Request().Context(), srv.xrpcc, &atproto.ServerCreateSession_Input{
		Identifier: srv.auth.Handle,
		Password:   srv.auth.Password,
	})
	if err != nil {
		return fmt.Errorf("failed to create new session: %w", err)
	}

	srv.auth.Token = session.AccessJwt
	srv.auth.RefreshToken = session.RefreshJwt

	// Extract expiry time from the token if possible
	expiry := extractTokenExpiry(session.AccessJwt)
	if expiry.IsZero() {
		// If we can't extract the expiry time, use a conservative default
		srv.auth.RefreshAt = time.Now().Add(time.Hour * 23) // Refresh 1 hour before assumed 24-hour expiry
		slog.Warn("could not extract token expiry time, using default refresh time")
	} else {
		// Set refresh time to 30 minutes before expiry
		srv.auth.RefreshAt = expiry.Add(-30 * time.Minute)
		slog.Info("extracted token expiry time", "expiry", expiry)
	}

	srv.xrpcc.Auth = &xrpc.AuthInfo{AccessJwt: session.AccessJwt}
	slog.Info("new session created successfully",
		"refresh_at", srv.auth.RefreshAt,
		"refresh_in", srv.auth.RefreshAt.Sub(time.Now()),
		"token_expiry", expiry)
	return nil
}

// Portfolio represents a user's portfolio data
type Portfolio struct {
	Handle      string    `json:"handle"`
	Description string    `json:"description"`
	Projects    []Project `json:"projects"`
}

// Project represents a portfolio project
type Project struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	URL         string `json:"url,omitempty"`
	ImageURL    string `json:"imageUrl,omitempty"`
}

// handleGetPortfolioConfig returns the current portfolio configuration
func (srv *Server) handleGetPortfolioConfig(c echo.Context) error {
	// Ensure we have a valid token before making any API requests
	// This is not currently used for portfolio config, but might be needed in the future
	if err := srv.ensureValidToken(c); err != nil {
		slog.Error("failed to ensure valid token", "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Authentication error: "+err.Error())
	}

	config := PortfolioConfig{
		Enabled: srv.enablePortfolio,
	}
	return c.JSON(http.StatusOK, config)
}

// handleGetPortfolio returns a user's portfolio data
func (srv *Server) handleGetPortfolio(c echo.Context) error {
	if !srv.enablePortfolio {
		return echo.NewHTTPError(http.StatusNotFound, "portfolio feature is not enabled")
	}

	// Get handle from URL param or hostname
	handle := c.Param("handle")
	if handle == "" {
		handle = getHandleFromRequest(c)
	}

	// Validate handle
	if err := srv.validateHandle(handle); err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "invalid handle")
	}

	// Ensure we have a valid token before making any API requests
	// This is not currently used for portfolio data, but might be needed in the future
	if err := srv.ensureValidToken(c); err != nil {
		slog.Error("failed to ensure valid token", "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Authentication error: "+err.Error())
	}

	// For now, return a placeholder portfolio
	// In a real implementation, this would fetch from a database or other storage
	portfolio := Portfolio{
		Handle:      handle,
		Description: "My portfolio of projects and contributions",
		Projects: []Project{
			{
				Title:       "Example Project",
				Description: "A sample project to demonstrate the portfolio feature",
				URL:         "https://example.com/project",
			},
		},
	}

	return c.JSON(http.StatusOK, portfolio)
}
