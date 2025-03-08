package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/labstack/echo/v4"
)

type GenericStatus struct {
	Status string `json:"status"`
	Daemon string `json:"daemon"`
}

func (srv *Server) HandleHealthCheck(c echo.Context) error {
	return c.JSON(200, GenericStatus{Status: "ok", Daemon: "athome"})
}

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

func (srv *Server) handleGetProfile(c echo.Context) error {
	handle := getHandleFromRequest(c)
	if handle == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "handle is required")
	}

	// Parse handle to ensure it's valid
	h, err := syntax.ParseHandle(handle)
	if err != nil {
		slog.Error("invalid handle format", "error", err)
		return echo.NewHTTPError(http.StatusBadRequest, "invalid handle format")
	}

	// Validate handle against allowed list
	if err := srv.validateHandle(handle); err != nil {
		slog.Error("handle not allowed", "error", err)
		return echo.NewHTTPError(http.StatusForbidden, err.Error())
	}

	// Look up the handle to get the DID
	ident, err := srv.dir.LookupHandle(c.Request().Context(), h)
	if err != nil {
		slog.Error("failed to lookup handle", "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	// Get profile using DID
	profile, err := bsky.ActorGetProfile(c.Request().Context(), srv.xrpcc, ident.DID.String())
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

func (srv *Server) handleGetFeed(c echo.Context) error {
	handle := getHandleFromRequest(c)
	if handle == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "handle is required")
	}

	// Parse handle to ensure it's valid
	h, err := syntax.ParseHandle(handle)
	if err != nil {
		slog.Error("invalid handle format", "error", err)
		return echo.NewHTTPError(http.StatusBadRequest, "invalid handle format")
	}

	// Validate handle against allowed list
	if err := srv.validateHandle(handle); err != nil {
		slog.Error("handle not allowed", "error", err)
		return echo.NewHTTPError(http.StatusForbidden, err.Error())
	}

	// Look up the handle to get the DID
	ident, err := srv.dir.LookupHandle(c.Request().Context(), h)
	if err != nil {
		slog.Error("failed to lookup handle", "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	cursor := c.QueryParam("cursor")
	slog.Info("fetching feed", "did", ident.DID.String(), "cursor", cursor)

	// Get feed using DID
	feed, err := bsky.FeedGetAuthorFeed(c.Request().Context(), srv.xrpcc, ident.DID.String(), cursor, "posts_no_replies", false, 20)
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

func (srv *Server) handleIndex(c echo.Context) error {
	nonce := c.Get("nonce").(string)

	// Read the Vite-built index.html
	content, err := os.ReadFile("public/index.html")
	if err != nil {
		slog.Error("failed to read index.html", "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to read index.html")
	}

	// Get the default handle from host
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

	//Add the index.html tile using the handle
	modifiedContent = strings.ReplaceAll(modifiedContent, "<title>AtHome</title>", "<title>@"+defaultHandle+"</title>")

	// Set proper content type
	c.Response().Header().Set(echo.HeaderContentType, echo.MIMETextHTMLCharsetUTF8)
	return c.HTMLBlob(http.StatusOK, []byte(modifiedContent))
}
