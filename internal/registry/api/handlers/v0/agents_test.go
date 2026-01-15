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

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
	"github.com/agentregistry-dev/agentregistry/internal/models"
	v0 "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0"
	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	"github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListAgentsEndpoint(t *testing.T) {
	ctx := context.Background()
	registryService := service.NewRegistryService(database.NewTestDB(t), config.NewConfig(), true)

	// Setup test data
	_, err := registryService.CreateAgent(ctx, &models.AgentJSON{
		AgentManifest: common.AgentManifest{
			Name:        "com.example.agent-alpha",
			Description: "Alpha test agent",
			Language:    "python",
			Framework:   "adk",
		},
		Version: "1.0.0",
	})
	require.NoError(t, err)
	// Approve and publish the agents so they're visible via public endpoints
	err = registryService.ApproveAgent(ctx, "com.example.agent-alpha", "1.0.0", "Test approval")
	require.NoError(t, err)
	err = registryService.PublishAgent(ctx, "com.example.agent-alpha", "1.0.0")
	require.NoError(t, err)

	_, err = registryService.CreateAgent(ctx, &models.AgentJSON{
		AgentManifest: common.AgentManifest{
			Name:        "com.example.agent-beta",
			Description: "Beta test agent",
			Language:    "python",
			Framework:   "adk",
		},
		Version: "2.0.0",
	})
	require.NoError(t, err)
	// Approve and publish the agents so they're visible via public endpoints
	err = registryService.ApproveAgent(ctx, "com.example.agent-beta", "2.0.0", "Test approval")
	require.NoError(t, err)
	err = registryService.PublishAgent(ctx, "com.example.agent-beta", "2.0.0")
	require.NoError(t, err)

	// Create API
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	v0.RegisterAgentsEndpoints(api, "/v0", registryService, false)

	tests := []struct {
		name           string
		queryParams    string
		expectedStatus int
		expectedCount  int
		expectedError  string
	}{
		{
			name:           "list all agents",
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
			name:           "search agents",
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
			req := httptest.NewRequest(http.MethodGet, "/v0/agents"+tt.queryParams, nil)
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedStatus == http.StatusOK {
				var resp models.AgentListResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				assert.NoError(t, err)
				assert.Len(t, resp.Agents, tt.expectedCount)
				assert.Equal(t, tt.expectedCount, resp.Metadata.Count)

				// Verify structure
				for _, agent := range resp.Agents {
					assert.NotEmpty(t, agent.Agent.Name)
					assert.NotEmpty(t, agent.Agent.Description)
					assert.NotNil(t, agent.Meta.Official)
				}
			} else if tt.expectedError != "" {
				assert.Contains(t, w.Body.String(), tt.expectedError)
			}
		})
	}
}

func TestGetLatestAgentVersionEndpoint(t *testing.T) {
	ctx := context.Background()
	registryService := service.NewRegistryService(database.NewTestDB(t), config.NewConfig(), true)

	// Setup test data
	_, err := registryService.CreateAgent(ctx, &models.AgentJSON{
		AgentManifest: common.AgentManifest{
			Name:        "com.example.detail-agent",
			Description: "Agent for detail testing",
			Language:    "python",
			Framework:   "adk",
		},
		Version: "1.0.0",
	})
	require.NoError(t, err)
	// Approve and publish the agent so it's visible via public endpoints
	err = registryService.ApproveAgent(ctx, "com.example.detail-agent", "1.0.0", "Test approval")
	require.NoError(t, err)
	err = registryService.PublishAgent(ctx, "com.example.detail-agent", "1.0.0")
	require.NoError(t, err)

	// Create API
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	v0.RegisterAgentsEndpoints(api, "/v0", registryService, false)

	tests := []struct {
		name           string
		agentName      string
		expectedStatus int
		expectedError  string
	}{
		{
			name:           "get existing agent latest version",
			agentName:      "com.example.detail-agent",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "get non-existent agent",
			agentName:      "com.example.non-existent",
			expectedStatus: http.StatusNotFound,
			expectedError:  "Agent not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// URL encode the agent name
			encodedName := url.PathEscape(tt.agentName)
			req := httptest.NewRequest(http.MethodGet, "/v0/agents/"+encodedName+"/versions/latest", nil)
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedStatus == http.StatusOK {
				var resp models.AgentResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				assert.NoError(t, err)
				assert.Equal(t, tt.agentName, resp.Agent.Name)
				assert.NotNil(t, resp.Meta.Official)
			} else if tt.expectedError != "" {
				assert.Contains(t, w.Body.String(), tt.expectedError)
			}
		})
	}
}

func TestGetAgentVersionEndpoint(t *testing.T) {
	ctx := context.Background()
	registryService := service.NewRegistryService(database.NewTestDB(t), config.NewConfig(), true)

	agentName := "com.example.version-agent"

	// Setup test data with multiple versions
	_, err := registryService.CreateAgent(ctx, &models.AgentJSON{
		AgentManifest: common.AgentManifest{
			Name:        agentName,
			Description: "Version test agent v1",
			Language:    "python",
			Framework:   "adk",
		},
		Version: "1.0.0",
	})
	require.NoError(t, err)
	// Approve and publish the agent so it's visible via public endpoints
	err = registryService.ApproveAgent(ctx, agentName, "1.0.0", "Test approval")
	require.NoError(t, err)
	err = registryService.PublishAgent(ctx, agentName, "1.0.0")
	require.NoError(t, err)

	_, err = registryService.CreateAgent(ctx, &models.AgentJSON{
		AgentManifest: common.AgentManifest{
			Name:        agentName,
			Description: "Version test agent v2",
			Language:    "python",
			Framework:   "adk",
		},
		Version: "2.0.0",
	})
	require.NoError(t, err)
	err = registryService.ApproveAgent(ctx, agentName, "2.0.0", "Test approval")
	require.NoError(t, err)
	err = registryService.PublishAgent(ctx, agentName, "2.0.0")
	require.NoError(t, err)

	// Add version with build metadata for URL encoding test
	_, err = registryService.CreateAgent(ctx, &models.AgentJSON{
		AgentManifest: common.AgentManifest{
			Name:        agentName,
			Description: "Version test agent with build metadata",
			Language:    "python",
			Framework:   "adk",
		},
		Version: "1.0.0+20130313144700",
	})
	require.NoError(t, err)
	err = registryService.ApproveAgent(ctx, agentName, "1.0.0+20130313144700", "Test approval")
	require.NoError(t, err)
	err = registryService.PublishAgent(ctx, agentName, "1.0.0+20130313144700")
	require.NoError(t, err)

	// Create API
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	v0.RegisterAgentsEndpoints(api, "/v0", registryService, false)

	tests := []struct {
		name           string
		agentName      string
		version        string
		expectedStatus int
		expectedError  string
		checkResult    func(*testing.T, *models.AgentResponse)
	}{
		{
			name:           "get existing version",
			agentName:      agentName,
			version:        "1.0.0",
			expectedStatus: http.StatusOK,
			checkResult: func(t *testing.T, resp *models.AgentResponse) {
				t.Helper()
				assert.Equal(t, "1.0.0", resp.Agent.Version)
				assert.Equal(t, "Version test agent v1", resp.Agent.Description)
				assert.False(t, resp.Meta.Official.IsLatest)
			},
		},
		{
			name:           "get latest version",
			agentName:      agentName,
			version:        "2.0.0",
			expectedStatus: http.StatusOK,
			checkResult: func(t *testing.T, resp *models.AgentResponse) {
				t.Helper()
				assert.Equal(t, "2.0.0", resp.Agent.Version)
				assert.True(t, resp.Meta.Official.IsLatest)
			},
		},
		{
			name:           "get non-existent version",
			agentName:      agentName,
			version:        "3.0.0",
			expectedStatus: http.StatusNotFound,
			expectedError:  "Agent not found",
		},
		{
			name:           "get non-existent agent",
			agentName:      "com.example.non-existent",
			version:        "1.0.0",
			expectedStatus: http.StatusNotFound,
			expectedError:  "Agent not found",
		},
		{
			name:           "get version with build metadata (URL encoded)",
			agentName:      agentName,
			version:        "1.0.0+20130313144700",
			expectedStatus: http.StatusOK,
			checkResult: func(t *testing.T, resp *models.AgentResponse) {
				t.Helper()
				assert.Equal(t, "1.0.0+20130313144700", resp.Agent.Version)
				assert.Equal(t, "Version test agent with build metadata", resp.Agent.Description)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// URL encode the agent name and version
			encodedName := url.PathEscape(tt.agentName)
			encodedVersion := url.PathEscape(tt.version)
			req := httptest.NewRequest(http.MethodGet, "/v0/agents/"+encodedName+"/versions/"+encodedVersion, nil)
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedStatus == http.StatusOK {
				var resp models.AgentResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				assert.NoError(t, err)
				assert.Equal(t, tt.agentName, resp.Agent.Name)
				assert.Equal(t, tt.version, resp.Agent.Version)
				assert.NotNil(t, resp.Meta.Official)

				if tt.checkResult != nil {
					tt.checkResult(t, &resp)
				}
			} else if tt.expectedError != "" {
				assert.Contains(t, w.Body.String(), tt.expectedError)
			}
		})
	}
}

func TestGetAllAgentVersionsEndpoint(t *testing.T) {
	ctx := context.Background()
	registryService := service.NewRegistryService(database.NewTestDB(t), config.NewConfig(), true)

	agentName := "com.example.multi-version-agent"

	// Setup test data with multiple versions
	versions := []string{"1.0.0", "1.1.0", "2.0.0"}
	for _, version := range versions {
		_, err := registryService.CreateAgent(ctx, &models.AgentJSON{
			AgentManifest: common.AgentManifest{
				Name:        agentName,
				Description: "Multi-version test agent " + version,
				Language:    "python",
				Framework:   "adk",
			},
			Version: version,
		})
		require.NoError(t, err)
		// Approve and publish each version so it's visible via public endpoints
		err = registryService.ApproveAgent(ctx, agentName, version, "Test approval reason")
		require.NoError(t, err)
		err = registryService.PublishAgent(ctx, agentName, version)
		require.NoError(t, err)
	}

	// Create API
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	v0.RegisterAgentsEndpoints(api, "/v0", registryService, false)

	tests := []struct {
		name           string
		agentName      string
		expectedStatus int
		expectedCount  int
		expectedError  string
	}{
		{
			name:           "get all versions of existing agent",
			agentName:      agentName,
			expectedStatus: http.StatusOK,
			expectedCount:  3,
		},
		{
			name:           "get versions of non-existent agent",
			agentName:      "com.example.non-existent",
			expectedStatus: http.StatusNotFound,
			expectedError:  "Agent not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// URL encode the agent name
			encodedName := url.PathEscape(tt.agentName)
			req := httptest.NewRequest(http.MethodGet, "/v0/agents/"+encodedName+"/versions", nil)
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedStatus == http.StatusOK {
				var resp models.AgentListResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				assert.NoError(t, err)
				assert.Len(t, resp.Agents, tt.expectedCount)
				assert.Equal(t, tt.expectedCount, resp.Metadata.Count)

				// Verify all versions are for the same agent
				for _, agent := range resp.Agents {
					assert.Equal(t, tt.agentName, agent.Agent.Name)
					assert.NotNil(t, agent.Meta.Official)
				}

				// Verify all expected versions are present
				versionSet := make(map[string]bool)
				for _, agent := range resp.Agents {
					versionSet[agent.Agent.Version] = true
				}
				for _, expectedVersion := range versions {
					assert.True(t, versionSet[expectedVersion], "Version %s should be present", expectedVersion)
				}

				// Verify exactly one is marked as latest
				latestCount := 0
				for _, agent := range resp.Agents {
					if agent.Meta.Official.IsLatest {
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

func TestAgentsEndpointEdgeCases(t *testing.T) {
	ctx := context.Background()
	registryService := service.NewRegistryService(database.NewTestDB(t), config.NewConfig(), true)

	// Setup test data with edge case names that comply with constraints
	specialAgents := []struct {
		name        string
		description string
		version     string
	}{
		{"io.dots.and-dashes.agent-name", "Agent with dots and dashes", "1.0.0"},
		{"com.long-namespace-name.very-long-agent-name-here", "Long names", "1.0.0"},
		{"org.test123.agent-with-dashes", "Agent with dashes", "1.0.0"},
	}

	for _, agent := range specialAgents {
		_, err := registryService.CreateAgent(ctx, &models.AgentJSON{
			AgentManifest: common.AgentManifest{
				Name:        agent.name,
				Description: agent.description,
				Language:    "python",
				Framework:   "adk",
			},
			Version: agent.version,
		})
		require.NoError(t, err)
		// Approve and publish each agent so it's visible via public endpoints
		err = registryService.ApproveAgent(ctx, agent.name, agent.version, "Test approval reason")
		require.NoError(t, err)
		err = registryService.PublishAgent(ctx, agent.name, agent.version)
		require.NoError(t, err)
	}

	// Create API
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	v0.RegisterAgentsEndpoints(api, "/v0", registryService, false)

	t.Run("URL encoding edge cases", func(t *testing.T) {
		tests := []struct {
			name      string
			agentName string
		}{
			{"dots and dashes", "io.dots.and-dashes.agent-name"},
			{"long agent name", "com.long-namespace-name.very-long-agent-name-here"},
			{"dashes", "org.test123.agent-with-dashes"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				// Test latest version endpoint
				encodedName := url.PathEscape(tt.agentName)
				req := httptest.NewRequest(http.MethodGet, "/v0/agents/"+encodedName+"/versions/latest", nil)
				w := httptest.NewRecorder()

				mux.ServeHTTP(w, req)

				assert.Equal(t, http.StatusOK, w.Code)

				var resp models.AgentResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				assert.NoError(t, err)
				assert.Equal(t, tt.agentName, resp.Agent.Name)
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
			{"combined valid parameters", "?search=agent&limit=5&version=latest", http.StatusOK, ""},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, "/v0/agents"+tt.queryParams, nil)
				w := httptest.NewRecorder()

				mux.ServeHTTP(w, req)

				assert.Equal(t, tt.expectedStatus, w.Code)

				if tt.expectedStatus == http.StatusOK {
					var resp models.AgentListResponse
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
		req := httptest.NewRequest(http.MethodGet, "/v0/agents", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

		var resp models.AgentListResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		assert.NoError(t, err)

		// Verify metadata structure
		assert.NotNil(t, resp.Metadata)
		assert.GreaterOrEqual(t, resp.Metadata.Count, 0)

		// Verify each agent has complete structure
		for _, agent := range resp.Agents {
			assert.NotEmpty(t, agent.Agent.Name)
			assert.NotEmpty(t, agent.Agent.Description)
			assert.NotEmpty(t, agent.Agent.Version)
			assert.NotNil(t, agent.Meta)
			assert.NotNil(t, agent.Meta.Official)
			assert.NotZero(t, agent.Meta.Official.PublishedAt)
		}
	})
}

func TestDeleteAgentVersionEndpoint(t *testing.T) {
	ctx := context.Background()
	registryService := service.NewRegistryService(database.NewTestDB(t), config.NewConfig(), true)

	agentName := "com.example.delete-agent"
	version := "1.0.0"

	// Setup test data
	_, err := registryService.CreateAgent(ctx, &models.AgentJSON{
		AgentManifest: common.AgentManifest{
			Name:        agentName,
			Description: "Agent for deletion testing",
			Language:    "python",
			Framework:   "adk",
		},
		Version: version,
	})
	require.NoError(t, err)

	// Create API with admin mode enabled
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	v0.RegisterAgentsEndpoints(api, "/v0", registryService, true)

	t.Run("delete existing agent version", func(t *testing.T) {
		encodedName := url.PathEscape(agentName)
		encodedVersion := url.PathEscape(version)
		req := httptest.NewRequest(http.MethodDelete, "/v0/agents/"+encodedName+"/versions/"+encodedVersion, nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp v0.EmptyResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		assert.NoError(t, err)
		assert.Contains(t, resp.Message, "deleted successfully")

		// Verify agent is actually deleted
		_, err = registryService.GetAgentByNameAndVersion(ctx, agentName, version, false, false)
		assert.Error(t, err)
	})

	t.Run("delete non-existent agent", func(t *testing.T) {
		encodedName := url.PathEscape("com.example.non-existent")
		encodedVersion := url.PathEscape("1.0.0")
		req := httptest.NewRequest(http.MethodDelete, "/v0/agents/"+encodedName+"/versions/"+encodedVersion, nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "Agent not found")
	})
}

func TestAgentsPublishedAndApprovalStatus_AutoApproveDisabled(t *testing.T) {
	ctx := context.Background()
	// Auto-approval is enabled for this test
	registryService := service.NewRegistryService(database.NewTestDB(t), config.NewConfig(), false)

	// Create agents with different published/approval status combinations
	testCases := []struct {
		name               string
		agentName          string
		version            string
		shouldApprove      bool
		shouldLeavePending bool // if true, the agent will not be approved or denied, leaving it in pending state
		shouldPublish      bool
		expectedVisible    bool // visible in public endpoints
	}{
		{"pending unpublished", "test.pending-unpublished", "1.0.0", false, true, false, false},
		{"pending published", "test.pending-published", "1.0.0", false, true, true, false},
		{"approved unpublished", "test.approved-unpublished", "1.0.0", true, false, false, false},
		{"approved published", "test.approved-published", "1.0.0", true, false, true, true},
		{"denied unpublished", "test.denied-unpublished", "1.0.0", false, false, false, false},
		{"denied published", "test.denied-published", "1.0.0", false, false, true, false},
	}

	// Create all agents
	for _, tc := range testCases {
		_, err := registryService.CreateAgent(ctx, &models.AgentJSON{
			AgentManifest: common.AgentManifest{
				Name:        tc.agentName,
				Description: tc.name,
				Language:    "python",
				Framework:   "adk",
			},
			Version: tc.version,
		})
		require.NoError(t, err, "Failed to create agent %s", tc.agentName)

		if !tc.shouldLeavePending {
			if tc.shouldApprove {
				err = registryService.ApproveAgent(ctx, tc.agentName, tc.version, "Test approval reason")
				require.NoError(t, err, "Failed to approve agent %s", tc.agentName)
			} else {
				err = registryService.DenyAgent(ctx, tc.agentName, tc.version, "Test denial reason")
				require.NoError(t, err, "Failed to deny agent %s", tc.agentName)
			}
		}

		if tc.shouldPublish {
			err = registryService.PublishAgent(ctx, tc.agentName, tc.version)
			require.NoError(t, err, "Failed to publish agent %s", tc.agentName)
		}
	}

	// Test public endpoints (should only show approved published)
	t.Run("public endpoints visibility", func(t *testing.T) {
		mux := http.NewServeMux()
		api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
		v0.RegisterAgentsEndpoints(api, "/v0", registryService, false)

		req := httptest.NewRequest(http.MethodGet, "/v0/agents", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp models.AgentListResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		assert.NoError(t, err)

		// Should only see approved + published agent
		visibleNames := make(map[string]bool)
		for _, agent := range resp.Agents {
			visibleNames[agent.Agent.Name] = true
		}

		for _, tc := range testCases {
			if tc.expectedVisible {
				assert.True(t, visibleNames[tc.agentName], "Agent %s should be visible in public endpoint", tc.agentName)
			} else {
				assert.False(t, visibleNames[tc.agentName], "Agent %s should NOT be visible in public endpoint", tc.agentName)
			}
		}
	})

	// Test admin endpoints (should show all agents)
	t.Run("admin endpoints visibility", func(t *testing.T) {
		mux := http.NewServeMux()
		api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
		v0.RegisterAgentsEndpoints(api, "/v0", registryService, true)

		req := httptest.NewRequest(http.MethodGet, "/v0/agents", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp models.AgentListResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		assert.NoError(t, err)

		// Should see all agents
		visibleNames := make(map[string]bool)
		agentMap := make(map[string]*models.AgentResponse)
		for i := range resp.Agents {
			agent := &resp.Agents[i]
			visibleNames[agent.Agent.Name] = true
			agentMap[agent.Agent.Name] = agent
		}

		for _, tc := range testCases {
			assert.True(t, visibleNames[tc.agentName], "Agent %s should be visible in admin endpoint", tc.agentName)

			// Verify approval status from API response
			agent := agentMap[tc.agentName]
			require.NotNil(t, agent, "Agent %s should exist in response", tc.agentName)
			require.NotNil(t, agent.Meta.Official, "Agent %s should have official metadata", tc.agentName)

			if tc.shouldLeavePending {
				assert.Equal(t, "PENDING", agent.Meta.ApprovalStatus.Status, "Agent %s should have PENDING approval status", tc.agentName)
				assert.Nil(t, agent.Meta.ApprovalStatus.Reason, "Agent %s should have no approval reason", tc.agentName)
			} else if tc.shouldApprove {
				assert.Equal(t, "APPROVED", agent.Meta.ApprovalStatus.Status, "Agent %s should have APPROVED status", tc.agentName)
				assert.Equal(t, "Test approval reason", *agent.Meta.ApprovalStatus.Reason, "Agent %s should have the approval reason", tc.agentName)
			} else {
				assert.Equal(t, "DENIED", agent.Meta.ApprovalStatus.Status, "Agent %s should have DENIED status", tc.agentName)
				assert.Equal(t, "Test denial reason", *agent.Meta.ApprovalStatus.Reason, "Agent %s should have the denial reason", tc.agentName)
			}

			// Verify published status
			assert.Equal(t, tc.shouldPublish, agent.Meta.Official.Published, "Agent %s published status should match", tc.agentName)
		}
	})
}

func TestAgentsPublishedAndApprovalStatus_AutoApproveEnabled(t *testing.T) {
	ctx := context.Background()
	// Auto-approval is enabled for this test
	registryService := service.NewRegistryService(database.NewTestDB(t), config.NewConfig(), true)

	// Create agents with different published/approval status combinations
	testCases := []struct {
		name               string
		agentName          string
		version            string
		shouldApprove      bool
		shouldLeavePending bool // if true, the agent will not be approved or denied, technically leaving it in auto-approved state
		shouldPublish      bool
		expectedVisible    bool // visible in public endpoints
	}{
		{"pending unpublished", "test.pending-unpublished", "1.0.0", false, true, false, false},
		{"pending published", "test.pending-published", "1.0.0", false, true, true, true}, // auto-approval is enabled
		{"approved unpublished", "test.approved-unpublished", "1.0.0", true, false, false, false},
		{"approved published", "test.approved-published", "1.0.0", true, false, true, true},
		{"denied unpublished", "test.denied-unpublished", "1.0.0", false, false, false, false},
		{"denied published", "test.denied-published", "1.0.0", false, false, true, false},
	}

	// Create all agents
	for _, tc := range testCases {
		_, err := registryService.CreateAgent(ctx, &models.AgentJSON{
			AgentManifest: common.AgentManifest{
				Name:        tc.agentName,
				Description: tc.name,
				Language:    "python",
				Framework:   "adk",
			},
			Version: tc.version,
		})
		require.NoError(t, err, "Failed to create agent %s", tc.agentName)

		if !tc.shouldLeavePending {
			if tc.shouldApprove {
				err = registryService.ApproveAgent(ctx, tc.agentName, tc.version, "Test approval reason")
				require.NoError(t, err, "Failed to approve agent %s", tc.agentName)
			} else {
				err = registryService.DenyAgent(ctx, tc.agentName, tc.version, "Test denial reason")
				require.NoError(t, err, "Failed to deny agent %s", tc.agentName)
			}
		}

		if tc.shouldPublish {
			err = registryService.PublishAgent(ctx, tc.agentName, tc.version)
			require.NoError(t, err, "Failed to publish agent %s", tc.agentName)
		}
	}

	// Test public endpoints (should only show approved published)
	t.Run("public endpoints visibility", func(t *testing.T) {
		mux := http.NewServeMux()
		api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
		v0.RegisterAgentsEndpoints(api, "/v0", registryService, false)

		req := httptest.NewRequest(http.MethodGet, "/v0/agents", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp models.AgentListResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		assert.NoError(t, err)

		// Should only see approved + published agent
		visibleNames := make(map[string]bool)
		for _, agent := range resp.Agents {
			visibleNames[agent.Agent.Name] = true
		}

		for _, tc := range testCases {
			if tc.expectedVisible {
				assert.True(t, visibleNames[tc.agentName], "Agent %s should be visible in public endpoint", tc.agentName)
			} else {
				assert.False(t, visibleNames[tc.agentName], "Agent %s should NOT be visible in public endpoint", tc.agentName)
			}
		}
	})

	// Test admin endpoints (should show all agents)
	t.Run("admin endpoints visibility", func(t *testing.T) {
		mux := http.NewServeMux()
		api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
		v0.RegisterAgentsEndpoints(api, "/v0", registryService, true)

		req := httptest.NewRequest(http.MethodGet, "/v0/agents", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp models.AgentListResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		assert.NoError(t, err)

		// Should see all agents
		visibleNames := make(map[string]bool)
		agentMap := make(map[string]*models.AgentResponse)
		for i := range resp.Agents {
			agent := &resp.Agents[i]
			visibleNames[agent.Agent.Name] = true
			agentMap[agent.Agent.Name] = agent
		}

		for _, tc := range testCases {
			assert.True(t, visibleNames[tc.agentName], "Agent %s should be visible in admin endpoint", tc.agentName)

			// Verify approval status from API response
			agent := agentMap[tc.agentName]
			require.NotNil(t, agent, "Agent %s should exist in response", tc.agentName)
			require.NotNil(t, agent.Meta.Official, "Agent %s should have official metadata", tc.agentName)

			if tc.shouldLeavePending {
				assert.Equal(t, "APPROVED", agent.Meta.ApprovalStatus.Status, "Agent %s should have APPROVED approval status", tc.agentName)
				assert.Equal(t, "Auto-approved: auto-approval is enabled", *agent.Meta.ApprovalStatus.Reason, "Agent %s should have the auto-approval reason", tc.agentName)
			} else if tc.shouldApprove {
				assert.Equal(t, "APPROVED", agent.Meta.ApprovalStatus.Status, "Agent %s should have APPROVED status", tc.agentName)
				assert.Equal(t, "Test approval reason", *agent.Meta.ApprovalStatus.Reason, "Agent %s should have the approval reason", tc.agentName)
			} else {
				assert.Equal(t, "DENIED", agent.Meta.ApprovalStatus.Status, "Agent %s should have DENIED status", tc.agentName)
				assert.Equal(t, "Test denial reason", *agent.Meta.ApprovalStatus.Reason, "Agent %s should have the denial reason", tc.agentName)
			}

			// Verify published status
			assert.Equal(t, tc.shouldPublish, agent.Meta.Official.Published, "Agent %s published status should match", tc.agentName)
		}
	})
}

func TestAgentsApprovalEndpoints_AutoApproveDisabled(t *testing.T) {
	ctx := context.Background()
	registryService := service.NewRegistryService(database.NewTestDB(t), config.NewConfig(), false)

	agentName := "test.approval-agent"
	version := "1.0.0"

	// Create agent
	_, err := registryService.CreateAgent(ctx, &models.AgentJSON{
		AgentManifest: common.AgentManifest{
			Name:        agentName,
			Description: "Agent for approval testing",
			Language:    "python",
			Framework:   "adk",
		},
		Version: version,
	})
	require.NoError(t, err)

	// Create API with admin endpoints
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	v0.RegisterAgentsEndpoints(api, "/v0", registryService, true)
	v0.RegisterAdminAgentsApprovalStatusEndpoints(api, "/v0", registryService)

	// Verify initial status is PENDING
	initialReq := httptest.NewRequest(http.MethodGet, "/v0/agents/"+url.PathEscape(agentName)+"/versions/"+url.PathEscape(version), nil)
	initialW := httptest.NewRecorder()
	mux.ServeHTTP(initialW, initialReq)
	assert.Equal(t, http.StatusOK, initialW.Code)
	var initialResp models.AgentResponse
	err = json.NewDecoder(initialW.Body).Decode(&initialResp)
	assert.NoError(t, err)
	assert.Equal(t, "PENDING", initialResp.Meta.ApprovalStatus.Status, "New agent should have PENDING approval status")
	assert.Nil(t, initialResp.Meta.ApprovalStatus.Reason, "New agent should have no approval reason")

	t.Run("approve agent", func(t *testing.T) {
		encodedName := url.PathEscape(agentName)
		encodedVersion := url.PathEscape(version)

		body := map[string]string{"reason": "Test approval reason"}
		bodyJSON, err := json.Marshal(body)
		require.NoError(t, err)
		approveReq := httptest.NewRequest(http.MethodPost, "/v0/agents/"+encodedName+"/versions/"+encodedVersion+"/approve", bytes.NewReader(bodyJSON))
		approveReq.Header.Set("Content-Type", "application/json")
		approveW := httptest.NewRecorder()

		mux.ServeHTTP(approveW, approveReq)

		assert.Equal(t, http.StatusOK, approveW.Code)
		var approveResp v0.EmptyResponse
		err = json.NewDecoder(approveW.Body).Decode(&approveResp)
		assert.NoError(t, err)
		assert.Contains(t, approveResp.Message, "approved successfully")

		// Verify approval status by checking the agent via admin endpoint
		verifyReq := httptest.NewRequest(http.MethodGet, "/v0/agents/"+encodedName+"/versions/"+encodedVersion, nil)
		verifyW := httptest.NewRecorder()
		mux.ServeHTTP(verifyW, verifyReq)

		assert.Equal(t, http.StatusOK, verifyW.Code)
		var verifyResp models.AgentResponse
		err = json.NewDecoder(verifyW.Body).Decode(&verifyResp)
		assert.NoError(t, err)
		assert.Equal(t, "APPROVED", verifyResp.Meta.ApprovalStatus.Status, "Agent should have APPROVED status after approval endpoint call")
		assert.Equal(t, "Test approval reason", *verifyResp.Meta.ApprovalStatus.Reason, "Agent should have the approval reason after approval endpoint call")
	})

	t.Run("deny agent", func(t *testing.T) {
		// Create another agent for denial test
		agentName2 := "test.denial-agent"
		_, err := registryService.CreateAgent(ctx, &models.AgentJSON{
			AgentManifest: common.AgentManifest{
				Name:        agentName2,
				Description: "Agent for denial testing",
				Language:    "python",
				Framework:   "adk",
			},
			Version: version,
		})
		require.NoError(t, err)

		encodedName := url.PathEscape(agentName2)
		encodedVersion := url.PathEscape(version)

		body := map[string]string{"reason": "Test denial reason"}
		bodyJSON, err := json.Marshal(body)
		require.NoError(t, err)
		denyReq := httptest.NewRequest(http.MethodPost, "/v0/agents/"+encodedName+"/versions/"+encodedVersion+"/deny", bytes.NewReader(bodyJSON))
		denyReq.Header.Set("Content-Type", "application/json")
		denyW := httptest.NewRecorder()

		mux.ServeHTTP(denyW, denyReq)

		assert.Equal(t, http.StatusOK, denyW.Code)
		var denyResp v0.EmptyResponse
		err = json.NewDecoder(denyW.Body).Decode(&denyResp)
		assert.NoError(t, err)
		assert.Contains(t, denyResp.Message, "denied successfully")

		// Verify approval status by checking the agent via admin endpoint
		verifyReq := httptest.NewRequest(http.MethodGet, "/v0/agents/"+encodedName+"/versions/"+encodedVersion, nil)
		verifyW := httptest.NewRecorder()
		mux.ServeHTTP(verifyW, verifyReq)

		assert.Equal(t, http.StatusOK, verifyW.Code)
		var verifyResp models.AgentResponse
		err = json.NewDecoder(verifyW.Body).Decode(&verifyResp)
		assert.NoError(t, err)
		assert.Equal(t, "DENIED", verifyResp.Meta.ApprovalStatus.Status, "Agent should have DENIED status after deny endpoint call")
		assert.Equal(t, "Test denial reason", *verifyResp.Meta.ApprovalStatus.Reason, "Agent should have the denial reason after deny endpoint call")
	})
}

func TestAgentsApprovalEndpoints_AutoApproveEnabled(t *testing.T) {
	ctx := context.Background()
	registryService := service.NewRegistryService(database.NewTestDB(t), config.NewConfig(), true)

	agentName := "test.approval-agent"
	version := "1.0.0"

	// Create agent
	_, err := registryService.CreateAgent(ctx, &models.AgentJSON{
		AgentManifest: common.AgentManifest{
			Name:        agentName,
			Description: "Agent for approval testing",
			Language:    "python",
			Framework:   "adk",
		},
		Version: version,
	})
	require.NoError(t, err)

	// Create API with admin endpoints
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	v0.RegisterAgentsEndpoints(api, "/v0", registryService, true)
	v0.RegisterAdminAgentsApprovalStatusEndpoints(api, "/v0", registryService)

	// Verify initial status is APPROVED
	initialReq := httptest.NewRequest(http.MethodGet, "/v0/agents/"+url.PathEscape(agentName)+"/versions/"+url.PathEscape(version), nil)
	initialW := httptest.NewRecorder()
	mux.ServeHTTP(initialW, initialReq)
	assert.Equal(t, http.StatusOK, initialW.Code)
	var initialResp models.AgentResponse
	err = json.NewDecoder(initialW.Body).Decode(&initialResp)
	assert.NoError(t, err)
	assert.Equal(t, "APPROVED", initialResp.Meta.ApprovalStatus.Status, "New agent should have APPROVED approval status")
	assert.Equal(t, "Auto-approved: auto-approval is enabled", *initialResp.Meta.ApprovalStatus.Reason, "New agent should have the auto-approval reason")

	t.Run("approve agent", func(t *testing.T) {
		encodedName := url.PathEscape(agentName)
		encodedVersion := url.PathEscape(version)

		body := map[string]string{"reason": "Test approval reason"}
		bodyJSON, err := json.Marshal(body)
		require.NoError(t, err)
		approveReq := httptest.NewRequest(http.MethodPost, "/v0/agents/"+encodedName+"/versions/"+encodedVersion+"/approve", bytes.NewReader(bodyJSON))
		approveReq.Header.Set("Content-Type", "application/json")
		approveW := httptest.NewRecorder()

		mux.ServeHTTP(approveW, approveReq)

		assert.Equal(t, http.StatusOK, approveW.Code)
		var approveResp v0.EmptyResponse
		err = json.NewDecoder(approveW.Body).Decode(&approveResp)
		assert.NoError(t, err)
		assert.Contains(t, approveResp.Message, "approved successfully")

		// Verify approval status by checking the agent via admin endpoint
		verifyReq := httptest.NewRequest(http.MethodGet, "/v0/agents/"+encodedName+"/versions/"+encodedVersion, nil)
		verifyW := httptest.NewRecorder()
		mux.ServeHTTP(verifyW, verifyReq)

		assert.Equal(t, http.StatusOK, verifyW.Code)
		var verifyResp models.AgentResponse
		err = json.NewDecoder(verifyW.Body).Decode(&verifyResp)
		assert.NoError(t, err)
		assert.Equal(t, "APPROVED", verifyResp.Meta.ApprovalStatus.Status, "Agent should have APPROVED status after approval endpoint call")
		assert.Equal(t, "Test approval reason", *verifyResp.Meta.ApprovalStatus.Reason, "Agent should have the approval reason after approval endpoint call")
	})

	t.Run("deny agent", func(t *testing.T) {
		// Create another agent for denial test
		agentName2 := "test.denial-agent"
		_, err := registryService.CreateAgent(ctx, &models.AgentJSON{
			AgentManifest: common.AgentManifest{
				Name:        agentName2,
				Description: "Agent for denial testing",
				Language:    "python",
				Framework:   "adk",
			},
			Version: version,
		})
		require.NoError(t, err)

		encodedName := url.PathEscape(agentName2)
		encodedVersion := url.PathEscape(version)

		body := map[string]string{"reason": "Test denial reason"}
		bodyJSON, err := json.Marshal(body)
		require.NoError(t, err)
		denyReq := httptest.NewRequest(http.MethodPost, "/v0/agents/"+encodedName+"/versions/"+encodedVersion+"/deny", bytes.NewReader(bodyJSON))
		denyReq.Header.Set("Content-Type", "application/json")
		denyW := httptest.NewRecorder()

		mux.ServeHTTP(denyW, denyReq)

		assert.Equal(t, http.StatusOK, denyW.Code)
		var denyResp v0.EmptyResponse
		err = json.NewDecoder(denyW.Body).Decode(&denyResp)
		assert.NoError(t, err)
		assert.Contains(t, denyResp.Message, "denied successfully")

		// Verify approval status by checking the agent via admin endpoint
		verifyReq := httptest.NewRequest(http.MethodGet, "/v0/agents/"+encodedName+"/versions/"+encodedVersion, nil)
		verifyW := httptest.NewRecorder()
		mux.ServeHTTP(verifyW, verifyReq)

		assert.Equal(t, http.StatusOK, verifyW.Code)
		var verifyResp models.AgentResponse
		err = json.NewDecoder(verifyW.Body).Decode(&verifyResp)
		assert.NoError(t, err)
		assert.Equal(t, "DENIED", verifyResp.Meta.ApprovalStatus.Status, "Agent should have DENIED status after deny endpoint call")
		assert.Equal(t, "Test denial reason", *verifyResp.Meta.ApprovalStatus.Reason, "Agent should have the denial reason after deny endpoint call")
	})
}

func TestAgentsDeploymentRequiresApproval(t *testing.T) {
	ctx := context.Background()
	testDB := database.NewTestDB(t)
	registryService := service.NewRegistryService(testDB, &config.Config{
		AgentGatewayPort: 21212,
	}, false) // Auto-approval is disabled for testing

	// Create API
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	v0.RegisterAdminAgentsApprovalStatusEndpoints(api, "/v0", registryService)
	v0.RegisterAgentsEndpoints(api, "/v0", registryService, true)
	v0.RegisterDeploymentsEndpoints(api, "/v0", registryService)

	agentName := "com.example.test-agent"
	version := "1.0.0"

	// Create agent
	_, err := registryService.CreateAgent(ctx, &models.AgentJSON{
		AgentManifest: common.AgentManifest{
			Name:        agentName,
			Description: "Agent for deployment approval testing",
		},
		Version: version,
	})
	require.NoError(t, err)

	t.Run("Deploy requires approval - returns 404 for unapproved agents", func(t *testing.T) {
		// Publish first
		err = registryService.PublishAgent(ctx, agentName, version)
		require.NoError(t, err)

		payload := map[string]interface{}{
			"serverName":   agentName,
			"version":      version,
			"resourceType": "agent",
		}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/v0/deployments", bytes.NewReader(body))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		// NotFound since we filter agents by published and approved
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("Cannot change approval while deployed", func(t *testing.T) {
		approvePayload := map[string]interface{}{
			"reason": "approved for deployment test",
		}
		approveBody, _ := json.Marshal(approvePayload)
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/v0/agents/%s/versions/%s/approve", url.PathEscape(agentName), version), bytes.NewReader(approveBody))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		// Mock deployment record directly in DB to avoid ReconcileAll
		err = testDB.CreateDeployment(ctx, nil, &models.Deployment{
			ServerName:   agentName,
			Version:      version,
			Status:       "active",
			ResourceType: "agent",
		})
		require.NoError(t, err)

		// Try to deny
		denyPayload := map[string]interface{}{
			"reason": "trying to deny deployed agent",
		}
		denyBody, _ := json.Marshal(denyPayload)
		req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/v0/agents/%s/versions/%s/deny", url.PathEscape(agentName), version), bytes.NewReader(denyBody))
		w = httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusConflict, w.Code)
		assert.Contains(t, w.Body.String(), "Cannot change approval status while artifact is deployed")
	})
}
