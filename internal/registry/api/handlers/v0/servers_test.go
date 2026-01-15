package v0_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/models"
	v0 "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0"
	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	"github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListServersEndpoint(t *testing.T) {
	ctx := context.Background()
	registryService := service.NewRegistryService(database.NewTestDB(t), config.NewConfig(), true)

	// Setup test data
	_, err := registryService.CreateServer(ctx, &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        "com.example/server-alpha",
		Description: "Alpha test server",
		Version:     "1.0.0",
	})
	require.NoError(t, err)

	_, err = registryService.CreateServer(ctx, &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        "com.example/server-beta",
		Description: "Beta test server",
		Version:     "2.0.0",
	})
	require.NoError(t, err)

	// Create API
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	v0.RegisterServersEndpoints(api, "/v0", registryService, false)

	tests := []struct {
		name           string
		queryParams    string
		expectedStatus int
		expectedCount  int
		expectedError  string
	}{
		{
			name:           "list all servers",
			queryParams:    "",
			expectedStatus: http.StatusOK,
			expectedCount:  2,
		},
		{
			name:           "list with limit",
			queryParams:    "?limit=1",
			expectedStatus: http.StatusOK,
			expectedCount:  1,
		},
		{
			name:           "search servers",
			queryParams:    "?search=alpha",
			expectedStatus: http.StatusOK,
			expectedCount:  1,
		},
		{
			name:           "filter latest only",
			queryParams:    "?version=latest",
			expectedStatus: http.StatusOK,
			expectedCount:  2,
		},
		{
			name:           "invalid limit",
			queryParams:    "?limit=abc",
			expectedStatus: http.StatusUnprocessableEntity,
			expectedError:  "validation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Skip("Skipping servers test")
			req := httptest.NewRequest(http.MethodGet, "/v0/servers"+tt.queryParams, nil)
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedStatus == http.StatusOK {
				var resp apiv0.ServerListResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				assert.NoError(t, err)
				assert.Len(t, resp.Servers, tt.expectedCount)
				assert.Equal(t, tt.expectedCount, resp.Metadata.Count)

				// Verify structure
				for _, server := range resp.Servers {
					assert.NotEmpty(t, server.Server.Name)
					assert.NotEmpty(t, server.Server.Description)
					assert.NotNil(t, server.Meta.Official)
				}
			} else if tt.expectedError != "" {
				assert.Contains(t, w.Body.String(), tt.expectedError)
			}
		})
	}
}

func TestGetLatestServerVersionEndpoint(t *testing.T) {
	ctx := context.Background()
	registryService := service.NewRegistryService(database.NewTestDB(t), config.NewConfig(), true)

	// Setup test data
	_, err := registryService.CreateServer(ctx, &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        "com.example/detail-server",
		Description: "Server for detail testing",
		Version:     "1.0.0",
	})
	require.NoError(t, err)
	// Approve and publish the server so it's visible via public endpoints
	err = registryService.ApproveServer(ctx, "com.example/detail-server", "1.0.0", "Test approval reason")
	require.NoError(t, err)
	err = registryService.PublishServer(ctx, "com.example/detail-server", "1.0.0")
	require.NoError(t, err)

	// Create API
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	v0.RegisterServersEndpoints(api, "/v0", registryService, false)

	tests := []struct {
		name           string
		serverName     string
		expectedStatus int
		expectedError  string
	}{
		{
			name:           "get existing server latest version",
			serverName:     "com.example/detail-server",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "get non-existent server",
			serverName:     "com.example/non-existent",
			expectedStatus: http.StatusNotFound,
			expectedError:  "Server not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// URL encode the server name
			encodedName := url.PathEscape(tt.serverName)
			req := httptest.NewRequest(http.MethodGet, "/v0/servers/"+encodedName+"/versions/latest", nil)
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedStatus == http.StatusOK {
				var resp apiv0.ServerListResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				assert.NoError(t, err)
				require.Len(t, resp.Servers, 1, "Should return exactly one server")
				server := resp.Servers[0]
				assert.Equal(t, tt.serverName, server.Server.Name)
				assert.NotNil(t, server.Meta.Official)
			} else if tt.expectedError != "" {
				assert.Contains(t, w.Body.String(), tt.expectedError)
			}
		})
	}
}

func TestGetServerVersionEndpoint(t *testing.T) {
	ctx := context.Background()
	registryService := service.NewRegistryService(database.NewTestDB(t), config.NewConfig(), true)

	serverName := "com.example/version-server"

	// Setup test data with multiple versions
	_, err := registryService.CreateServer(ctx, &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        serverName,
		Description: "Version test server v1",
		Version:     "1.0.0",
	})
	require.NoError(t, err)
	// Approve and publish the server so it's visible via public endpoints
	err = registryService.ApproveServer(ctx, serverName, "1.0.0", "Test approval")
	require.NoError(t, err)
	err = registryService.PublishServer(ctx, serverName, "1.0.0")
	require.NoError(t, err)

	_, err = registryService.CreateServer(ctx, &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        serverName,
		Description: "Version test server v2",
		Version:     "2.0.0",
	})
	require.NoError(t, err)
	err = registryService.ApproveServer(ctx, serverName, "2.0.0", "Test approval")
	require.NoError(t, err)
	err = registryService.PublishServer(ctx, serverName, "2.0.0")
	require.NoError(t, err)

	// Add version with build metadata for URL encoding test
	_, err = registryService.CreateServer(ctx, &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        serverName,
		Description: "Version test server with build metadata",
		Version:     "1.0.0+20130313144700",
	})
	require.NoError(t, err)
	err = registryService.ApproveServer(ctx, serverName, "1.0.0+20130313144700", "Test approval")
	require.NoError(t, err)
	err = registryService.PublishServer(ctx, serverName, "1.0.0+20130313144700")
	require.NoError(t, err)

	// Create API
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	v0.RegisterServersEndpoints(api, "/v0", registryService, false)

	tests := []struct {
		name           string
		serverName     string
		version        string
		expectedStatus int
		expectedError  string
		checkResult    func(*testing.T, *apiv0.ServerResponse)
	}{
		{
			name:           "get existing version",
			serverName:     serverName,
			version:        "1.0.0",
			expectedStatus: http.StatusOK,
			checkResult: func(t *testing.T, resp *apiv0.ServerResponse) {
				t.Helper()
				assert.Equal(t, "1.0.0", resp.Server.Version)
				assert.Equal(t, "Version test server v1", resp.Server.Description)
				assert.False(t, resp.Meta.Official.IsLatest)
			},
		},
		{
			name:           "get latest version",
			serverName:     serverName,
			version:        "2.0.0",
			expectedStatus: http.StatusOK,
			checkResult: func(t *testing.T, resp *apiv0.ServerResponse) {
				t.Helper()
				assert.Equal(t, "2.0.0", resp.Server.Version)
				assert.True(t, resp.Meta.Official.IsLatest)
			},
		},
		{
			name:           "get non-existent version",
			serverName:     serverName,
			version:        "3.0.0",
			expectedStatus: http.StatusNotFound,
			expectedError:  "Server not found",
		},
		{
			name:           "get non-existent server",
			serverName:     "com.example/non-existent",
			version:        "1.0.0",
			expectedStatus: http.StatusNotFound,
			expectedError:  "Server not found",
		},
		{
			name:           "get version with build metadata (URL encoded)",
			serverName:     serverName,
			version:        "1.0.0+20130313144700",
			expectedStatus: http.StatusOK,
			checkResult: func(t *testing.T, resp *apiv0.ServerResponse) {
				t.Helper()
				assert.Equal(t, "1.0.0+20130313144700", resp.Server.Version)
				assert.Equal(t, "Version test server with build metadata", resp.Server.Description)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// URL encode the server name and version
			encodedName := url.PathEscape(tt.serverName)
			encodedVersion := url.PathEscape(tt.version)
			req := httptest.NewRequest(http.MethodGet, "/v0/servers/"+encodedName+"/versions/"+encodedVersion, nil)
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedStatus == http.StatusOK {
				var resp apiv0.ServerListResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				assert.NoError(t, err)
				require.Len(t, resp.Servers, 1, "Should return exactly one server")
				server := resp.Servers[0]
				assert.Equal(t, tt.serverName, server.Server.Name)
				assert.Equal(t, tt.version, server.Server.Version)
				assert.NotNil(t, server.Meta.Official)

				if tt.checkResult != nil {
					tt.checkResult(t, &server)
				}
			} else if tt.expectedError != "" {
				assert.Contains(t, w.Body.String(), tt.expectedError)
			}
		})
	}
}

func TestGetServerReadmeEndpoints(t *testing.T) {
	ctx := context.Background()
	registryService := service.NewRegistryService(database.NewTestDB(t), config.NewConfig(), true)

	serverName := "com.example/readme-endpoint"
	_, err := registryService.CreateServer(ctx, &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        serverName,
		Description: "Server with README",
		Version:     "1.0.0",
	})
	require.NoError(t, err)

	err = registryService.StoreServerReadme(ctx, serverName, "1.0.0", []byte("# Title\nBody"), "text/markdown")
	require.NoError(t, err)

	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	v0.RegisterServersEndpoints(api, "/v0", registryService, false)

	t.Run("latest readme", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v0/servers/"+url.PathEscape(serverName)+"/readme", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp v0.ServerReadmeResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, "# Title\nBody", resp.Content)
		assert.Equal(t, "text/markdown", resp.ContentType)
		assert.Equal(t, "1.0.0", resp.Version)
		assert.NotEmpty(t, resp.Sha256)
	})

	t.Run("version readme", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v0/servers/"+url.PathEscape(serverName)+"/versions/1.0.0/readme", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp v0.ServerReadmeResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, "# Title\nBody", resp.Content)
	})

	t.Run("missing readme", func(t *testing.T) {
		otherServer := "com.example/no-readme"
		_, err := registryService.CreateServer(ctx, &apiv0.ServerJSON{
			Schema:      model.CurrentSchemaURL,
			Name:        otherServer,
			Description: "Server without README",
			Version:     "1.0.0",
		})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/v0/servers/"+url.PathEscape(otherServer)+"/readme", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "README not found")
	})
}

func TestGetAllVersionsEndpoint(t *testing.T) {
	ctx := context.Background()
	registryService := service.NewRegistryService(database.NewTestDB(t), config.NewConfig(), true)

	serverName := "com.example/multi-version-server"

	// Setup test data with multiple versions
	versions := []string{"1.0.0", "1.1.0", "2.0.0"}
	for _, version := range versions {
		_, err := registryService.CreateServer(ctx, &apiv0.ServerJSON{
			Schema:      model.CurrentSchemaURL,
			Name:        serverName,
			Description: "Multi-version test server " + version,
			Version:     version,
		})
		require.NoError(t, err)
		// Approve and publish each version so it's visible via public endpoints
		err = registryService.ApproveServer(ctx, serverName, version, "Test approval")
		require.NoError(t, err)
		err = registryService.PublishServer(ctx, serverName, version)
		require.NoError(t, err)
	}

	// Create API
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	v0.RegisterServersEndpoints(api, "/v0", registryService, false)

	tests := []struct {
		name           string
		serverName     string
		expectedStatus int
		expectedCount  int
		expectedError  string
	}{
		{
			name:           "get all versions of existing server",
			serverName:     serverName,
			expectedStatus: http.StatusOK,
			expectedCount:  3,
		},
		{
			name:           "get versions of non-existent server",
			serverName:     "com.example/non-existent",
			expectedStatus: http.StatusNotFound,
			expectedError:  "Server not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// URL encode the server name
			encodedName := url.PathEscape(tt.serverName)
			req := httptest.NewRequest(http.MethodGet, "/v0/servers/"+encodedName+"/versions", nil)
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedStatus == http.StatusOK {
				var resp apiv0.ServerListResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				assert.NoError(t, err)
				assert.Len(t, resp.Servers, tt.expectedCount)
				assert.Equal(t, tt.expectedCount, resp.Metadata.Count)

				// Verify all versions are for the same server
				for _, server := range resp.Servers {
					assert.Equal(t, tt.serverName, server.Server.Name)
					assert.NotNil(t, server.Meta.Official)
				}

				// Verify all expected versions are present
				versionSet := make(map[string]bool)
				for _, server := range resp.Servers {
					versionSet[server.Server.Version] = true
				}
				for _, expectedVersion := range versions {
					assert.True(t, versionSet[expectedVersion], "Version %s should be present", expectedVersion)
				}

				// Verify exactly one is marked as latest
				latestCount := 0
				for _, server := range resp.Servers {
					if server.Meta.Official.IsLatest {
						latestCount++
					}
				}
				assert.Equal(t, 1, latestCount, "Exactly one version should be marked as latest")
			} else if tt.expectedError != "" {
				assert.Contains(t, w.Body.String(), tt.expectedError)
			}
		})
	}
}

func TestServersEndpointEdgeCases(t *testing.T) {
	ctx := context.Background()
	registryService := service.NewRegistryService(database.NewTestDB(t), config.NewConfig(), true)

	// Setup test data with edge case names that comply with constraints
	specialServers := []struct {
		name        string
		description string
		version     string
	}{
		{"io.dots.and-dashes/server-name", "Server with dots and dashes", "1.0.0"},
		{"com.long-namespace-name/very-long-server-name-here", "Long names", "1.0.0"},
		{"org.test123/server_with_underscores", "Server with underscores", "1.0.0"},
	}

	for _, server := range specialServers {
		_, err := registryService.CreateServer(ctx, &apiv0.ServerJSON{
			Schema:      model.CurrentSchemaURL,
			Name:        server.name,
			Description: server.description,
			Version:     server.version,
		})
		require.NoError(t, err)
		// Approve and publish each server so it's visible via public endpoints
		err = registryService.ApproveServer(ctx, server.name, server.version, "Test approval reason")
		require.NoError(t, err)
		err = registryService.PublishServer(ctx, server.name, server.version)
		require.NoError(t, err)
	}

	// Create API
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	v0.RegisterServersEndpoints(api, "/v0", registryService, false)

	t.Run("URL encoding edge cases", func(t *testing.T) {
		tests := []struct {
			name       string
			serverName string
		}{
			{"dots and dashes", "io.dots.and-dashes/server-name"},
			{"long server name", "com.long-namespace-name/very-long-server-name-here"},
			{"underscores", "org.test123/server_with_underscores"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				// Test latest version endpoint
				encodedName := url.PathEscape(tt.serverName)
				req := httptest.NewRequest(http.MethodGet, "/v0/servers/"+encodedName+"/versions/latest", nil)
				w := httptest.NewRecorder()

				mux.ServeHTTP(w, req)

				assert.Equal(t, http.StatusOK, w.Code)

				var resp apiv0.ServerListResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				assert.NoError(t, err)
				require.Len(t, resp.Servers, 1, "Should return exactly one server")
				assert.Equal(t, tt.serverName, resp.Servers[0].Server.Name)
			})
		}
	})

	t.Run("query parameter edge cases", func(t *testing.T) {
		tests := []struct {
			name           string
			queryParams    string
			expectedStatus int
			expectedError  string
		}{
			{"limit too high", "?limit=1000", http.StatusUnprocessableEntity, "validation failed"},
			{"negative limit", "?limit=-1", http.StatusUnprocessableEntity, "validation failed"},
			{"invalid updated_since format", "?updated_since=invalid", http.StatusBadRequest, "Invalid updated_since format"},
			{"future updated_since", "?updated_since=2030-01-01T00:00:00Z", http.StatusOK, ""},
			{"very old updated_since", "?updated_since=1990-01-01T00:00:00Z", http.StatusOK, ""},
			{"empty search parameter", "?search=", http.StatusOK, ""},
			{"search with special characters", "?search=测试", http.StatusOK, ""},
			{"combined valid parameters", "?search=server&limit=5&version=latest", http.StatusOK, ""},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, "/v0/servers"+tt.queryParams, nil)
				w := httptest.NewRecorder()

				mux.ServeHTTP(w, req)

				assert.Equal(t, tt.expectedStatus, w.Code)

				if tt.expectedStatus == http.StatusOK {
					var resp apiv0.ServerListResponse
					err := json.NewDecoder(w.Body).Decode(&resp)
					assert.NoError(t, err)
					assert.NotNil(t, resp.Metadata)
				} else if tt.expectedError != "" {
					assert.Contains(t, w.Body.String(), tt.expectedError)
				}
			})
		}
	})

	t.Run("response structure validation", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v0/servers", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

		var resp apiv0.ServerListResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		assert.NoError(t, err)

		// Verify metadata structure
		assert.NotNil(t, resp.Metadata)
		assert.GreaterOrEqual(t, resp.Metadata.Count, 0)

		// Verify each server has complete structure
		for _, server := range resp.Servers {
			assert.NotEmpty(t, server.Server.Name)
			assert.NotEmpty(t, server.Server.Description)
			assert.NotEmpty(t, server.Server.Version)
			assert.NotNil(t, server.Meta)
			assert.NotNil(t, server.Meta.Official)
			assert.NotZero(t, server.Meta.Official.PublishedAt)
			assert.Contains(t, []model.Status{model.StatusActive, model.StatusDeprecated, model.StatusDeleted}, server.Meta.Official.Status)
		}
	})
}

func TestServersPublishedAndApprovalStatus_AutoApproveDisabled(t *testing.T) {
	ctx := context.Background()
	registryService := service.NewRegistryService(database.NewTestDB(t), config.NewConfig(), false)

	// Create servers with different published/approval status combinations
	testCases := []struct {
		name               string
		serverName         string
		version            string
		shouldApprove      bool
		shouldLeavePending bool // if true, the server will not be approved or denied, leaving it in pending state
		shouldPublish      bool
		expectedVisible    bool // visible in public endpoints
	}{
		{"pending unpublished", "com.example/pending-unpublished", "1.0.0", false, true, false, false},
		{"pending published", "com.example/pending-published", "1.0.0", false, true, true, false},
		{"approved unpublished", "com.example/approved-unpublished", "1.0.0", true, false, false, false},
		{"approved published", "com.example/approved-published", "1.0.0", true, false, true, true},
		{"denied unpublished", "com.example/denied-unpublished", "1.0.0", false, false, false, false},
		{"denied published", "com.example/denied-published", "1.0.0", false, false, true, false},
	}

	// Create all servers
	for _, tc := range testCases {
		_, err := registryService.CreateServer(ctx, &apiv0.ServerJSON{
			Schema:      model.CurrentSchemaURL,
			Name:        tc.serverName,
			Description: tc.name,
			Version:     tc.version,
		})
		require.NoError(t, err, "Failed to create server %s", tc.serverName)

		if !tc.shouldLeavePending {
			if tc.shouldApprove {
				err = registryService.ApproveServer(ctx, tc.serverName, tc.version, "Test approval reason")
				require.NoError(t, err, "Failed to approve server %s", tc.serverName)
			} else {
				err = registryService.DenyServer(ctx, tc.serverName, tc.version, "Test denial reason")
				require.NoError(t, err, "Failed to deny server %s", tc.serverName)
			}
		}

		if tc.shouldPublish {
			err = registryService.PublishServer(ctx, tc.serverName, tc.version)
			require.NoError(t, err, "Failed to publish server %s", tc.serverName)
		}
	}

	// Test public endpoints (should only show approved + published)
	t.Run("public endpoints visibility", func(t *testing.T) {
		mux := http.NewServeMux()
		api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
		v0.RegisterServersEndpoints(api, "/v0", registryService, false)

		req := httptest.NewRequest(http.MethodGet, "/v0/servers", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp apiv0.ServerListResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		assert.NoError(t, err)

		// Should only see approved + published server
		visibleNames := make(map[string]bool)
		for _, server := range resp.Servers {
			visibleNames[server.Server.Name] = true
		}

		for _, tc := range testCases {
			if tc.expectedVisible {
				assert.True(t, visibleNames[tc.serverName], "Server %s should be visible in public endpoint", tc.serverName)
			} else {
				assert.False(t, visibleNames[tc.serverName], "Server %s should NOT be visible in public endpoint", tc.serverName)
			}
		}
	})

	// Test admin endpoints (should show all servers)
	t.Run("admin endpoints visibility", func(t *testing.T) {
		mux := http.NewServeMux()
		api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
		v0.RegisterServersEndpoints(api, "/v0", registryService, true)

		req := httptest.NewRequest(http.MethodGet, "/v0/servers", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp apiv0.ServerListResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		assert.NoError(t, err)

		// Should see all servers
		visibleNames := make(map[string]bool)
		for _, server := range resp.Servers {
			visibleNames[server.Server.Name] = true
		}

		for _, tc := range testCases {
			assert.True(t, visibleNames[tc.serverName], "Server %s should be visible in admin endpoint", tc.serverName)
		}
	})
}

func TestServersPublishedAndApprovalStatus_AutoApproveEnabled(t *testing.T) {
	ctx := context.Background()
	registryService := service.NewRegistryService(database.NewTestDB(t), config.NewConfig(), true)

	// Create servers with different published/approval status combinations
	testCases := []struct {
		name               string
		serverName         string
		version            string
		shouldApprove      bool
		shouldLeavePending bool // if true, the server will not be approved or denied, leaving it in pending state
		shouldPublish      bool
		expectedVisible    bool // visible in public endpoints
	}{
		{"pending unpublished", "com.example/pending-unpublished", "1.0.0", false, true, false, false},
		{"pending published", "com.example/pending-published", "1.0.0", false, true, true, true}, // auto-approval is enabled
		{"approved unpublished", "com.example/approved-unpublished", "1.0.0", true, false, false, false},
		{"approved published", "com.example/approved-published", "1.0.0", true, false, true, true},
		{"denied unpublished", "com.example/denied-unpublished", "1.0.0", false, false, false, false},
		{"denied published", "com.example/denied-published", "1.0.0", false, false, true, false},
	}

	// Create all servers
	for _, tc := range testCases {
		_, err := registryService.CreateServer(ctx, &apiv0.ServerJSON{
			Schema:      model.CurrentSchemaURL,
			Name:        tc.serverName,
			Description: tc.name,
			Version:     tc.version,
		})
		require.NoError(t, err, "Failed to create server %s", tc.serverName)

		if !tc.shouldLeavePending {
			if tc.shouldApprove {
				err = registryService.ApproveServer(ctx, tc.serverName, tc.version, "Test approval reason")
				require.NoError(t, err, "Failed to approve server %s", tc.serverName)
			} else {
				err = registryService.DenyServer(ctx, tc.serverName, tc.version, "Test denial reason")
				require.NoError(t, err, "Failed to deny server %s", tc.serverName)
			}
		}

		if tc.shouldPublish {
			err = registryService.PublishServer(ctx, tc.serverName, tc.version)
			require.NoError(t, err, "Failed to publish server %s", tc.serverName)
		}
	}

	// Test public endpoints (should only show approved + published)
	t.Run("public endpoints visibility", func(t *testing.T) {
		mux := http.NewServeMux()
		api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
		v0.RegisterServersEndpoints(api, "/v0", registryService, false)

		req := httptest.NewRequest(http.MethodGet, "/v0/servers", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp apiv0.ServerListResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		assert.NoError(t, err)

		// Should only see approved + published server
		visibleNames := make(map[string]bool)
		for _, server := range resp.Servers {
			visibleNames[server.Server.Name] = true
		}

		for _, tc := range testCases {
			if tc.expectedVisible {
				assert.True(t, visibleNames[tc.serverName], "Server %s should be visible in public endpoint", tc.serverName)
			} else {
				assert.False(t, visibleNames[tc.serverName], "Server %s should NOT be visible in public endpoint", tc.serverName)
			}
		}
	})

	// Test admin endpoints (should show all servers)
	t.Run("admin endpoints visibility", func(t *testing.T) {
		mux := http.NewServeMux()
		api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
		v0.RegisterServersEndpoints(api, "/v0", registryService, true)

		req := httptest.NewRequest(http.MethodGet, "/v0/servers", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp apiv0.ServerListResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		assert.NoError(t, err)

		// Should see all servers
		visibleNames := make(map[string]bool)
		for _, server := range resp.Servers {
			visibleNames[server.Server.Name] = true
		}

		for _, tc := range testCases {
			assert.True(t, visibleNames[tc.serverName], "Server %s should be visible in admin endpoint", tc.serverName)
		}
	})
}

func TestServersApprovalEndpoints_AutoApproveDisabled(t *testing.T) {
	ctx := context.Background()
	registryService := service.NewRegistryService(database.NewTestDB(t), config.NewConfig(), false)

	serverName := "com.example/approval-server"
	version := "1.0.0"

	// Create server
	_, err := registryService.CreateServer(ctx, &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        serverName,
		Description: "Server for approval testing",
		Version:     version,
	})
	require.NoError(t, err)

	// Create API with admin endpoints
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	v0.RegisterServersEndpoints(api, "/v0", registryService, true)
	v0.RegisterAdminServersApprovalStatusEndpoints(api, "/v0", registryService)

	// Verify initial status is PENDING
	initialReq := httptest.NewRequest(http.MethodGet, "/v0/servers/"+url.PathEscape(serverName)+"/versions/"+url.PathEscape(version), nil)
	initialW := httptest.NewRecorder()
	mux.ServeHTTP(initialW, initialReq)
	assert.Equal(t, http.StatusOK, initialW.Code)
	var initialResp models.ServerListResponse
	err = json.NewDecoder(initialW.Body).Decode(&initialResp)
	assert.NoError(t, err)
	require.Len(t, initialResp.Servers, 1)
	assert.Equal(t, "PENDING", initialResp.Servers[0].Meta.ApprovalStatus.Status, "New server should have PENDING approval status")
	assert.Nil(t, initialResp.Servers[0].Meta.ApprovalStatus.Reason, "New server should have no approval reason")

	t.Run("approve server", func(t *testing.T) {
		encodedName := url.PathEscape(serverName)
		encodedVersion := url.PathEscape(version)

		body := map[string]string{"reason": "Test approval reason"}
		bodyJSON, err := json.Marshal(body)
		require.NoError(t, err)
		approveReq := httptest.NewRequest(http.MethodPost, "/v0/servers/"+encodedName+"/versions/"+encodedVersion+"/approve", bytes.NewReader(bodyJSON))
		approveReq.Header.Set("Content-Type", "application/json")
		approveW := httptest.NewRecorder()

		mux.ServeHTTP(approveW, approveReq)

		assert.Equal(t, http.StatusOK, approveW.Code)
		var resp v0.EmptyResponse
		err = json.NewDecoder(approveW.Body).Decode(&resp)
		assert.NoError(t, err)
		assert.Contains(t, resp.Message, "approved successfully")

		// Verify approval status by checking the server via admin endpoint
		verifyReq := httptest.NewRequest(http.MethodGet, "/v0/servers/"+encodedName+"/versions/"+encodedVersion, nil)
		verifyW := httptest.NewRecorder()
		mux.ServeHTTP(verifyW, verifyReq)

		assert.Equal(t, http.StatusOK, verifyW.Code)
		var verifyResp models.ServerListResponse
		err = json.NewDecoder(verifyW.Body).Decode(&verifyResp)
		assert.NoError(t, err)
		require.Len(t, verifyResp.Servers, 1)
		assert.Equal(t, "APPROVED", verifyResp.Servers[0].Meta.ApprovalStatus.Status, "Server should have APPROVED status after approval endpoint call")
		assert.Equal(t, "Test approval reason", *verifyResp.Servers[0].Meta.ApprovalStatus.Reason, "Server should have the approval reason after approval endpoint call")
	})

	t.Run("deny server", func(t *testing.T) {
		// Create another server for denial test
		serverName2 := "com.example/denial-server"
		_, err := registryService.CreateServer(ctx, &apiv0.ServerJSON{
			Schema:      model.CurrentSchemaURL,
			Name:        serverName2,
			Description: "Server for denial testing",
			Version:     version,
		})
		require.NoError(t, err)

		encodedName := url.PathEscape(serverName2)
		encodedVersion := url.PathEscape(version)

		body := map[string]string{"reason": "Test denial reason"}
		bodyJSON, err := json.Marshal(body)
		require.NoError(t, err)
		denyReq := httptest.NewRequest(http.MethodPost, "/v0/servers/"+encodedName+"/versions/"+encodedVersion+"/deny", bytes.NewReader(bodyJSON))
		denyReq.Header.Set("Content-Type", "application/json")
		denyW := httptest.NewRecorder()

		mux.ServeHTTP(denyW, denyReq)

		assert.Equal(t, http.StatusOK, denyW.Code)
		var denyResp v0.EmptyResponse
		err = json.NewDecoder(denyW.Body).Decode(&denyResp)
		assert.NoError(t, err)
		assert.Contains(t, denyResp.Message, "denied successfully")

		// Verify denial status by checking the server via admin endpoint
		encodedName2 := url.PathEscape(serverName2)
		verifyReq := httptest.NewRequest(http.MethodGet, "/v0/servers/"+encodedName2+"/versions/"+encodedVersion, nil)
		verifyW := httptest.NewRecorder()
		mux.ServeHTTP(verifyW, verifyReq)

		assert.Equal(t, http.StatusOK, verifyW.Code)
		var verifyResp models.ServerListResponse
		err = json.NewDecoder(verifyW.Body).Decode(&verifyResp)
		assert.NoError(t, err)
		require.Len(t, verifyResp.Servers, 1)
		assert.Equal(t, "DENIED", verifyResp.Servers[0].Meta.ApprovalStatus.Status, "Server should have DENIED status after deny endpoint call")
		assert.Equal(t, "Test denial reason", *verifyResp.Servers[0].Meta.ApprovalStatus.Reason, "Server should have the denial reason after deny endpoint call")
	})
}

func TestServersApprovalEndpoints_AutoApproveEnabled(t *testing.T) {
	ctx := context.Background()
	registryService := service.NewRegistryService(database.NewTestDB(t), config.NewConfig(), true)

	serverName := "com.example/approval-server"
	version := "1.0.0"

	// Create server
	_, err := registryService.CreateServer(ctx, &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        serverName,
		Description: "Server for approval testing",
		Version:     version,
	})
	require.NoError(t, err)

	// Create API with admin endpoints
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	v0.RegisterServersEndpoints(api, "/v0", registryService, true)
	v0.RegisterAdminServersApprovalStatusEndpoints(api, "/v0", registryService)

	// Verify initial status is PENDING
	initialReq := httptest.NewRequest(http.MethodGet, "/v0/servers/"+url.PathEscape(serverName)+"/versions/"+url.PathEscape(version), nil)
	initialW := httptest.NewRecorder()
	mux.ServeHTTP(initialW, initialReq)
	assert.Equal(t, http.StatusOK, initialW.Code)
	var initialResp models.ServerListResponse
	err = json.NewDecoder(initialW.Body).Decode(&initialResp)
	assert.NoError(t, err)
	require.Len(t, initialResp.Servers, 1)
	assert.Equal(t, "APPROVED", initialResp.Servers[0].Meta.ApprovalStatus.Status, "New server should have APPROVED approval status")
	assert.Equal(t, "Auto-approved: auto-approval is enabled", *initialResp.Servers[0].Meta.ApprovalStatus.Reason, "New server should have the auto-approval reason")

	t.Run("approve server", func(t *testing.T) {
		encodedName := url.PathEscape(serverName)
		encodedVersion := url.PathEscape(version)

		body := map[string]string{"reason": "Test approval reason"}
		bodyJSON, err := json.Marshal(body)
		require.NoError(t, err)
		approveReq := httptest.NewRequest(http.MethodPost, "/v0/servers/"+encodedName+"/versions/"+encodedVersion+"/approve", bytes.NewReader(bodyJSON))
		approveReq.Header.Set("Content-Type", "application/json")
		approveW := httptest.NewRecorder()

		mux.ServeHTTP(approveW, approveReq)

		assert.Equal(t, http.StatusOK, approveW.Code)
		var resp v0.EmptyResponse
		err = json.NewDecoder(approveW.Body).Decode(&resp)
		assert.NoError(t, err)
		assert.Contains(t, resp.Message, "approved successfully")

		// Verify approval status by checking the server via admin endpoint
		verifyReq := httptest.NewRequest(http.MethodGet, "/v0/servers/"+encodedName+"/versions/"+encodedVersion, nil)
		verifyW := httptest.NewRecorder()
		mux.ServeHTTP(verifyW, verifyReq)

		assert.Equal(t, http.StatusOK, verifyW.Code)
		var verifyResp models.ServerListResponse
		err = json.NewDecoder(verifyW.Body).Decode(&verifyResp)
		assert.NoError(t, err)
		require.Len(t, verifyResp.Servers, 1)
		assert.Equal(t, "APPROVED", verifyResp.Servers[0].Meta.ApprovalStatus.Status, "Server should have APPROVED status after approval endpoint call")
		assert.Equal(t, "Test approval reason", *verifyResp.Servers[0].Meta.ApprovalStatus.Reason, "Server should have the approval reason after approval endpoint call")
	})

	t.Run("deny server", func(t *testing.T) {
		// Create another server for denial test
		serverName2 := "com.example/denial-server"
		_, err := registryService.CreateServer(ctx, &apiv0.ServerJSON{
			Schema:      model.CurrentSchemaURL,
			Name:        serverName2,
			Description: "Server for denial testing",
			Version:     version,
		})
		require.NoError(t, err)

		encodedName := url.PathEscape(serverName2)
		encodedVersion := url.PathEscape(version)

		body := map[string]string{"reason": "Test denial reason"}
		bodyJSON, err := json.Marshal(body)
		require.NoError(t, err)
		denyReq := httptest.NewRequest(http.MethodPost, "/v0/servers/"+encodedName+"/versions/"+encodedVersion+"/deny", bytes.NewReader(bodyJSON))
		denyReq.Header.Set("Content-Type", "application/json")
		denyW := httptest.NewRecorder()

		mux.ServeHTTP(denyW, denyReq)

		assert.Equal(t, http.StatusOK, denyW.Code)
		var denyResp v0.EmptyResponse
		err = json.NewDecoder(denyW.Body).Decode(&denyResp)
		assert.NoError(t, err)
		assert.Contains(t, denyResp.Message, "denied successfully")

		// Verify denial status by checking the server via admin endpoint
		encodedName2 := url.PathEscape(serverName2)
		verifyReq := httptest.NewRequest(http.MethodGet, "/v0/servers/"+encodedName2+"/versions/"+encodedVersion, nil)
		verifyW := httptest.NewRecorder()
		mux.ServeHTTP(verifyW, verifyReq)

		assert.Equal(t, http.StatusOK, verifyW.Code)
		var verifyResp models.ServerListResponse
		err = json.NewDecoder(verifyW.Body).Decode(&verifyResp)
		assert.NoError(t, err)
		require.Len(t, verifyResp.Servers, 1)
		assert.Equal(t, "DENIED", verifyResp.Servers[0].Meta.ApprovalStatus.Status, "Server should have DENIED status after deny endpoint call")
		assert.Equal(t, "Test denial reason", *verifyResp.Servers[0].Meta.ApprovalStatus.Reason, "Server should have the denial reason after deny endpoint call")
	})
}

func TestServersDeploymentRequiresApproval(t *testing.T) {
	ctx := context.Background()
	testDB := database.NewTestDB(t)
	registryService := service.NewRegistryService(testDB, &config.Config{
		AgentGatewayPort: 21212,
	}, false) // Auto-approval is disabled for testing

	// Create API
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	v0.RegisterAdminServersApprovalStatusEndpoints(api, "/v0", registryService)
	v0.RegisterServersEndpoints(api, "/v0", registryService, true)
	v0.RegisterDeploymentsEndpoints(api, "/v0", registryService)

	serverName := "com.example/api-test-server"
	version := "1.0.0"

	// Create server
	_, err := registryService.CreateServer(ctx, &apiv0.ServerJSON{
		Schema:  model.CurrentSchemaURL,
		Name:    serverName,
		Version: version,
		Remotes: []model.Transport{
			{Type: "streamable-http", URL: "https://api.example.com/api-test"},
		},
	})
	require.NoError(t, err)

	t.Run("Deploy requires approval - returns 403 or similar through service error", func(t *testing.T) {
		// Publish first
		err = registryService.PublishServer(ctx, serverName, version)
		require.NoError(t, err)

		payload := map[string]interface{}{
			"serverName":   serverName,
			"version":      version,
			"resourceType": "mcp",
		}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/v0/deployments", bytes.NewReader(body))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		// NotFound since we filter the agents by published and approved
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("Cannot change approval while deployed", func(t *testing.T) {
		approvePayload := map[string]interface{}{
			"reason": "approved for deployment test",
		}
		approveBody, _ := json.Marshal(approvePayload)
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/v0/servers/%s/versions/%s/approve", url.PathEscape(serverName), version), bytes.NewReader(approveBody))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		// Mock deployment record directly in DB to avoid ReconcileAll
		err = testDB.CreateDeployment(ctx, nil, &models.Deployment{
			ServerName:   serverName,
			Version:      version,
			Status:       "active",
			ResourceType: "mcp",
		})
		require.NoError(t, err)

		// Try to deny
		denyPayload := map[string]interface{}{
			"reason": "trying to deny deployed server",
		}
		denyBody, _ := json.Marshal(denyPayload)
		req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/v0/servers/%s/versions/%s/deny", url.PathEscape(serverName), version), bytes.NewReader(denyBody))
		w = httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusConflict, w.Code)
		assert.Contains(t, w.Body.String(), "Cannot change approval status while artifact is deployed")
	})
}
