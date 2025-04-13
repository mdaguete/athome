package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/api/atproto"
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

// ensureValidToken ensures that the token is valid before making API requests.
// It forces a token refresh if the token is expired or about to expire.
func (srv *Server) ensureValidToken(c echo.Context) error {
	// Always force a token refresh before making API requests
	// This is a more aggressive approach to ensure we always have a valid token
	slog.Info("forcing token refresh before API request")
	return srv.refreshAuth(c)
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

// startBackgroundTokenRefresh starts a background goroutine that periodically checks
// if the token needs to be refreshed and refreshes it if necessary.
// This ensures that the token is always valid, even if there are no API requests.
func (srv *Server) startBackgroundTokenRefresh(ctx context.Context) {
	// Create a ticker that ticks every 5 minutes
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("stopping background token refresh")
			return
		case <-ticker.C:
			// Log that we're checking token expiry
			slog.Info("background refresh: checking if token needs refresh")

			// Check if we need to refresh
			srv.authMutex.RLock()
			tokenExpired := srv.auth.RefreshAt.IsZero() || time.Now().After(srv.auth.RefreshAt.Add(-30*time.Minute))
			slog.Info("background refresh: token expiry check result",
				"token_expired", tokenExpired,
				"refresh_at", srv.auth.RefreshAt,
				"now", time.Now(),
				"time_until_refresh", srv.auth.RefreshAt.Sub(time.Now()))
			srv.authMutex.RUnlock()

			if !tokenExpired {
				slog.Info("background refresh: token is still valid, no refresh needed")
			} else {
				// Try to refresh using the refresh token first
				srv.authMutex.RLock()
				hasRefreshToken := srv.auth.RefreshToken != ""
				refreshToken := srv.auth.RefreshToken
				srv.authMutex.RUnlock()

				var newAccessToken, newRefreshToken string
				var refreshSuccess bool

				if hasRefreshToken {
					slog.Info("background refresh: attempting to use refresh token")

					// Save the current auth info
					tempAuth := srv.xrpcc.Auth

					// Set the refresh token in the Auth field
					srv.xrpcc.Auth = &xrpc.AuthInfo{RefreshJwt: refreshToken}

					// Try to refresh the session
					refreshedSession, err := atproto.ServerRefreshSession(ctx, srv.xrpcc)

					// Restore the original auth
					srv.xrpcc.Auth = tempAuth

					if err == nil {
						newAccessToken = refreshedSession.AccessJwt
						newRefreshToken = refreshedSession.RefreshJwt
						refreshSuccess = true
						slog.Info("background refresh: successfully refreshed using refresh token")
					} else {
						slog.Error("background refresh: failed to refresh using token, falling back to password auth", "error", err)
					}
				}

				// If refresh token didn't work or isn't available, create a new session
				if !refreshSuccess {
					slog.Info("background refresh: creating new session")
					session, err := atproto.ServerCreateSession(ctx, srv.xrpcc, &atproto.ServerCreateSession_Input{
						Identifier: srv.auth.Handle,
						Password:   srv.auth.Password,
					})
					if err != nil {
						slog.Error("background refresh: failed to create new session", "error", err)
						continue
					}

					newAccessToken = session.AccessJwt
					newRefreshToken = session.RefreshJwt
					slog.Info("background refresh: successfully created new session")
				}

				// Update token info under lock
				srv.authMutex.Lock()
				srv.auth.Token = newAccessToken
				srv.auth.RefreshToken = newRefreshToken

				// Extract expiry time from the token if possible
				expiry := extractTokenExpiry(newAccessToken)
				if expiry.IsZero() {
					// If we can't extract the expiry time, use a conservative default
					srv.auth.RefreshAt = time.Now().Add(time.Hour * 23) // Refresh 1 hour before assumed 24-hour expiry
					slog.Warn("background refresh: could not extract token expiry time, using default refresh time")
				} else {
					// Set refresh time to 30 minutes before expiry
					srv.auth.RefreshAt = expiry.Add(-30 * time.Minute)
					slog.Info("background refresh: extracted token expiry time", "expiry", expiry)
				}

				srv.xrpcc.Auth = &xrpc.AuthInfo{AccessJwt: newAccessToken}
				srv.authMutex.Unlock()

				slog.Info("background token refresh completed successfully",
					"refresh_at", srv.auth.RefreshAt,
					"refresh_in", srv.auth.RefreshAt.Sub(time.Now()),
					"token_expiry", expiry)
			}
		}
	}
}
