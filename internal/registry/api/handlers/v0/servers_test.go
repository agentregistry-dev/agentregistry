package v0_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	v0 "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0"
	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	internaldb "github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/internal/registry/embeddings"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/jackc/pgx/v5"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const semanticEmbeddingDimensions = 1536

func TestListServersEndpoint(t *testing.T) {
	testSeed := make([]byte, ed25519.SeedSize)
	_, randErr := rand.Read(testSeed)
	require.NoError(t, randErr)
	testConfig := &config.Config{
		JWTPrivateKey:            hex.EncodeToString(testSeed),
		EnableRegistryValidation: false, // Disable for unit tests
	}

	ctx := context.Background()
	registryService := service.NewRegistryService(internaldb.NewTestDB(t), testConfig, nil)

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
	v0.RegisterServersEndpoints(api, "/v0", registryService)

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
				var resp models.ServerListResponse
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

func TestListServersSemanticSearch(t *testing.T) {
	ctx := context.Background()
	db := internaldb.NewTestDB(t)
	ensureVectorExtension(t, db)

	cfg := config.NewConfig()
	cfg.Embeddings.Enabled = true
	cfg.Embeddings.Provider = "stub"
	cfg.Embeddings.Model = "stub-model"

	testSeed := make([]byte, ed25519.SeedSize)
	_, randErr := rand.Read(testSeed)
	require.NoError(t, randErr)
	cfg.JWTPrivateKey = hex.EncodeToString(testSeed)
	cfg.EnableRegistryValidation = false // Disable for unit tests

	provider := newStubEmbeddingProvider(map[string][]float32{
		"server": {0.1, 0.95, 0.0},
	})

	registryService := service.NewRegistryService(db, cfg, provider)

	// Setup servers
	backupServer := "com.example/backup-server"
	weatherServer := "com.example/weather-server"

	for _, srv := range []struct {
		name        string
		description string
	}{
		{name: backupServer, description: "Handles filesystem backups"},
		{name: weatherServer, description: "Provides detailed weather forecasts"},
	} {
		_, err := registryService.CreateServer(ctx, &apiv0.ServerJSON{
			Schema:      model.CurrentSchemaURL,
			Name:        srv.name,
			Description: srv.description,
			Version:     "1.0.0",
		})
		require.NoError(t, err)
	}

	// Seed embeddings for deterministic ordering
	ctxWithAuth := internaldb.WithTestSession(ctx)
	require.NoError(t, registryService.UpsertServerEmbedding(ctxWithAuth, backupServer, "1.0.0", &database.SemanticEmbedding{
		Vector:     semanticVector(0.1, 0.9, 0.0),
		Provider:   "stub",
		Model:      "stub-model",
		Dimensions: 3,
		Checksum:   "backup",
		Generated:  time.Now().UTC(),
	}))
	require.NoError(t, registryService.UpsertServerEmbedding(ctxWithAuth, weatherServer, "1.0.0", &database.SemanticEmbedding{
		Vector:     semanticVector(0.9, 0.1, 0.0),
		Provider:   "stub",
		Model:      "stub-model",
		Dimensions: 3,
		Checksum:   "weather",
		Generated:  time.Now().UTC(),
	}))

	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	v0.RegisterServersEndpoints(api, "/v0", registryService)

	t.Run("semantic search ranks by similarity", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v0/servers?search=server&semantic_search=true", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)

		var resp models.ServerListResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		require.Len(t, resp.Servers, 2)

		assert.Equal(t, backupServer, resp.Servers[0].Server.Name, "backup server should rank first")
		require.NotNil(t, resp.Servers[0].Meta.Semantic, "semantic metadata should be present")
		assert.NotZero(t, resp.Servers[0].Meta.Semantic.Score)

		assert.Equal(t, []string{"server"}, provider.Queries())
	})

	t.Run("semantic search without search term is rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v0/servers?semantic_search=true", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "semantic_search requires the search parameter")
		assert.Equal(t, []string{"server"}, provider.Queries(), "provider should not be called for invalid requests")
	})
}

func TestGetLatestServerVersionEndpoint(t *testing.T) {
	testSeed := make([]byte, ed25519.SeedSize)
	_, randErr := rand.Read(testSeed)
	require.NoError(t, randErr)
	testConfig := &config.Config{
		JWTPrivateKey:            hex.EncodeToString(testSeed),
		EnableRegistryValidation: false, // Disable for unit tests
	}

	ctx := context.Background()
	registryService := service.NewRegistryService(internaldb.NewTestDB(t), testConfig, nil)

	// Setup test data
	_, err := registryService.CreateServer(ctx, &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        "com.example/detail-server",
		Description: "Server for detail testing",
		Version:     "1.0.0",
	})
	require.NoError(t, err)

	// Create API
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	v0.RegisterServersEndpoints(api, "/v0", registryService)

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
				var resp models.ServerListResponse
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
	testSeed := make([]byte, ed25519.SeedSize)
	_, randErr := rand.Read(testSeed)
	require.NoError(t, randErr)
	testConfig := &config.Config{
		JWTPrivateKey:            hex.EncodeToString(testSeed),
		EnableRegistryValidation: false, // Disable for unit tests
	}

	ctx := context.Background()
	registryService := service.NewRegistryService(internaldb.NewTestDB(t), testConfig, nil)

	serverName := "com.example/version-server"

	// Setup test data with multiple versions
	_, err := registryService.CreateServer(ctx, &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        serverName,
		Description: "Version test server v1",
		Version:     "1.0.0",
	})
	require.NoError(t, err)

	_, err = registryService.CreateServer(ctx, &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        serverName,
		Description: "Version test server v2",
		Version:     "2.0.0",
	})
	require.NoError(t, err)

	// Add version with build metadata for URL encoding test
	_, err = registryService.CreateServer(ctx, &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        serverName,
		Description: "Version test server with build metadata",
		Version:     "1.0.0+20130313144700",
	})
	require.NoError(t, err)

	// Create API
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	v0.RegisterServersEndpoints(api, "/v0", registryService)

	tests := []struct {
		name           string
		serverName     string
		version        string
		expectedStatus int
		expectedError  string
		checkResult    func(*testing.T, *models.ServerResponse)
	}{
		{
			name:           "get existing version",
			serverName:     serverName,
			version:        "1.0.0",
			expectedStatus: http.StatusOK,
			checkResult: func(t *testing.T, resp *models.ServerResponse) {
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
			checkResult: func(t *testing.T, resp *models.ServerResponse) {
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
			checkResult: func(t *testing.T, resp *models.ServerResponse) {
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
				var resp models.ServerListResponse
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

func ensureVectorExtension(t *testing.T, db database.Database) {
	t.Helper()
	err := db.InTransaction(context.Background(), func(ctx context.Context, tx pgx.Tx) error {
		_, execErr := tx.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS vector")
		return execErr
	})
	require.NoError(t, err, "failed to ensure pgvector extension for tests")
}

type stubEmbeddingProvider struct {
	mu       sync.Mutex
	vectors  map[string][]float32
	queries  []string
	provider string
	model    string
}

func newStubEmbeddingProvider(vectors map[string][]float32) *stubEmbeddingProvider {
	norm := make(map[string][]float32, len(vectors))
	for key, vec := range vectors {
		norm[key] = semanticVector(vec...)
	}
	return &stubEmbeddingProvider{
		vectors:  norm,
		provider: "stub",
		model:    "stub-model",
	}
}

func (s *stubEmbeddingProvider) Generate(ctx context.Context, payload embeddings.Payload) (*embeddings.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.queries = append(s.queries, payload.Text)
	base, ok := s.vectors[payload.Text]
	if !ok {
		base = semanticVector(1, 0, 0)
	}
	vec := append([]float32(nil), base...)

	return &embeddings.Result{
		Vector:      vec,
		Provider:    s.provider,
		Model:       s.model,
		Dimensions:  len(vec),
		GeneratedAt: time.Now().UTC(),
	}, nil
}

func (s *stubEmbeddingProvider) Queries() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.queries...)
}

func semanticVector(values ...float32) []float32 {
	vec := make([]float32, semanticEmbeddingDimensions)
	copy(vec, values)
	return vec
}

func TestGetServerReadmeEndpoints(t *testing.T) {
	testSeed := make([]byte, ed25519.SeedSize)
	_, randErr := rand.Read(testSeed)
	require.NoError(t, randErr)
	testConfig := &config.Config{
		JWTPrivateKey:            hex.EncodeToString(testSeed),
		EnableRegistryValidation: false, // Disable for unit tests
	}

	ctx := context.Background()
	registryService := service.NewRegistryService(internaldb.NewTestDB(t), testConfig, nil)

	serverName := "com.example/readme-endpoint"
	_, err := registryService.CreateServer(ctx, &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        serverName,
		Description: "Server with README",
		Version:     "1.0.0",
	})
	require.NoError(t, err)

	ctxWithAuth := internaldb.WithTestSession(ctx)
	err = registryService.StoreServerReadme(ctxWithAuth, serverName, "1.0.0", []byte("# Title\nBody"), "text/markdown")
	require.NoError(t, err)

	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	v0.RegisterServersEndpoints(api, "/v0", registryService)

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
	testSeed := make([]byte, ed25519.SeedSize)
	_, randErr := rand.Read(testSeed)
	require.NoError(t, randErr)
	testConfig := &config.Config{
		JWTPrivateKey:            hex.EncodeToString(testSeed),
		EnableRegistryValidation: false, // Disable for unit tests
	}

	ctx := context.Background()
	registryService := service.NewRegistryService(internaldb.NewTestDB(t), testConfig, nil)

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
	}

	// Create API
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	v0.RegisterServersEndpoints(api, "/v0", registryService)

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
				var resp models.ServerListResponse
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
	testSeed := make([]byte, ed25519.SeedSize)
	_, randErr := rand.Read(testSeed)
	require.NoError(t, randErr)
	testConfig := &config.Config{
		JWTPrivateKey:            hex.EncodeToString(testSeed),
		EnableRegistryValidation: false, // Disable for unit tests
	}

	ctx := context.Background()
	registryService := service.NewRegistryService(internaldb.NewTestDB(t), testConfig, nil)

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
	}

	// Create API
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	v0.RegisterServersEndpoints(api, "/v0", registryService)

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

				var resp models.ServerListResponse
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
					var resp models.ServerListResponse
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

		var resp models.ServerListResponse
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
