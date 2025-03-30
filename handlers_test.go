package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/xrpc"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockXRPCClient implements a mock XRPC client for testing
type mockXRPCClient struct {
	createSessionCalls int
	mu                 sync.Mutex
	shouldFail         bool
	failureCount       int
	simulatedDelay     time.Duration
}

func newMockXRPCClient() *xrpc.Client {
	mock := &mockXRPCClient{}
	return &xrpc.Client{
		Host: "https://mock.bsky.test",
		Auth: &xrpc.AuthInfo{},
		Client: &http.Client{
			Transport: mock,
		},
	}
}

// RoundTrip implements http.RoundTripper to intercept HTTP requests
func (m *mockXRPCClient) RoundTrip(req *http.Request) (*http.Response, error) {
	// Only handle createSession endpoint
	if strings.HasSuffix(req.URL.Path, "/xrpc/com.atproto.server.createSession") {
		m.mu.Lock()
		defer m.mu.Unlock()

		// Simulate delay if configured
		if m.simulatedDelay > 0 {
			time.Sleep(m.simulatedDelay)
		}

		m.createSessionCalls++

		// Check if we should fail this call
		if m.shouldFail && m.failureCount > 0 {
			m.failureCount--
			return nil, fmt.Errorf("simulated failure")
		}

		// Return mock response
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(strings.NewReader(`{
				"accessJwt": "mock-token-` + fmt.Sprint(m.createSessionCalls) + `",
				"refreshJwt": "mock-refresh-token",
				"handle": "test.handle",
				"did": "did:mock:user"
			}`)),
			Header: make(http.Header),
		}
		resp.Header.Set("Content-Type", "application/json")
		return resp, nil
	}

	return nil, fmt.Errorf("HTTP requests should not be made in tests")
}

func (m *mockXRPCClient) setSimulatedDelay(delay time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.simulatedDelay = delay
}

func (m *mockXRPCClient) setShouldFail(fail bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shouldFail = fail
}

func (m *mockXRPCClient) setFailureCount(count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failureCount = count
}

func (m *mockXRPCClient) getCreateSessionCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.createSessionCalls
}

func TestRefreshAuth_Concurrency(t *testing.T) {
	tests := []struct {
		name           string
		concurrency    int
		simulatedDelay time.Duration
		shouldFail     bool
		failureCount   int
		wantErr        bool
	}{
		{
			name:           "Multiple concurrent requests",
			concurrency:    10,
			simulatedDelay: 50 * time.Millisecond,
			shouldFail:     false,
			wantErr:        false,
		},
		{
			name:           "Handle intermittent failures",
			concurrency:    5,
			simulatedDelay: 20 * time.Millisecond,
			shouldFail:     true,
			failureCount:   2,
			wantErr:        true,
		},
		{
			name:         "All requests fail",
			concurrency:  3,
			shouldFail:   true,
			failureCount: 3,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock client and server
			mock := &mockXRPCClient{}
			mock.setSimulatedDelay(tt.simulatedDelay)
			mock.setShouldFail(tt.shouldFail)
			mock.setFailureCount(tt.failureCount)

			client := &xrpc.Client{
				Host: "https://mock.bsky.test",
				Auth: &xrpc.AuthInfo{},
				Client: &http.Client{
					Transport: mock,
				},
			}

			srv := &Server{
				e:         echo.New(),
				xrpcc:     client,
				auth:      &AuthConfig{Handle: "test.handle", Password: "test-pass"},
				authMutex: sync.RWMutex{},
			}

			// Create wait group for concurrent requests
			var wg sync.WaitGroup
			errors := make([]error, tt.concurrency)

			// Launch concurrent requests
			for i := 0; i < tt.concurrency; i++ {
				wg.Add(1)
				go func(index int) {
					defer wg.Done()

					// Create test request context
					req := httptest.NewRequest(http.MethodGet, "/", nil)
					rec := httptest.NewRecorder()
					c := srv.e.NewContext(req, rec)

					errors[index] = srv.refreshAuth(c)
				}(i)
			}

			// Wait for all requests to complete
			wg.Wait()

			// Verify results
			if tt.wantErr {
				hasError := false
				for _, err := range errors {
					if err != nil {
						hasError = true
						break
					}
				}
				assert.True(t, hasError, "expected at least one error")
			} else {
				for _, err := range errors {
					assert.NoError(t, err, "unexpected error")
				}
			}

			// Verify the number of actual API calls
			actualCalls := mock.getCreateSessionCalls()
			assert.True(t, actualCalls > 0, "expected at least one API call")
			assert.True(t, actualCalls <= tt.concurrency,
				"number of API calls (%d) should not exceed concurrency level (%d)",
				actualCalls, tt.concurrency)
		})
	}
}

func TestRefreshAuth_TokenExpiry(t *testing.T) {
	mock := &mockXRPCClient{}
	client := &xrpc.Client{
		Host: "https://mock.bsky.test",
		Auth: &xrpc.AuthInfo{},
		Client: &http.Client{
			Transport: mock,
		},
	}

	srv := &Server{
		e:         echo.New(),
		xrpcc:     client,
		auth:      &AuthConfig{Handle: "test.handle", Password: "test-pass"},
		authMutex: sync.RWMutex{},
	}

	// Create test request context
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := srv.e.NewContext(req, rec)

	// Test initial token creation
	err := srv.refreshAuth(c)
	require.NoError(t, err)
	initialToken := srv.auth.Token
	assert.NotEmpty(t, initialToken)

	// Verify token is not refreshed before expiry
	err = srv.refreshAuth(c)
	require.NoError(t, err)
	assert.Equal(t, initialToken, srv.auth.Token)
	assert.Equal(t, 1, mock.getCreateSessionCalls())

	// Simulate token expiry
	srv.auth.RefreshAt = time.Now().Add(-1 * time.Hour)

	// Verify token is refreshed after expiry
	err = srv.refreshAuth(c)
	require.NoError(t, err)
	assert.NotEqual(t, initialToken, srv.auth.Token)
	assert.Equal(t, 2, mock.getCreateSessionCalls())
}

func TestRefreshAuth_NoConfig(t *testing.T) {
	srv := &Server{
		e:    echo.New(),
		auth: nil,
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := srv.e.NewContext(req, rec)

	err := srv.refreshAuth(c)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no auth configuration")
}
