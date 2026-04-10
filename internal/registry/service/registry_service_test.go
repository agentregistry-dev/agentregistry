//nolint:testpackage
package service

import (
	"context"
	"fmt"
	"maps"
	"sync"
	"testing"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	internaldb "github.com/agentregistry-dev/agentregistry/internal/registry/database"
	api "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/types"
	agentsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/agent"
	deploymentsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/deployment"
	providersvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/provider"
	serversvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/server"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/auth"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	registrytypes "github.com/agentregistry-dev/agentregistry/pkg/types"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newRegistryTestServerService(storeDB database.Store, cfg *config.Config) serversvc.Registry {
	return serversvc.New(serversvc.Dependencies{StoreDB: storeDB, Config: cfg})
}

func TestValidateNoDuplicateRemoteURLs(t *testing.T) {
	ctx := context.Background()

	// Create test data
	existingServers := map[string]*apiv0.ServerJSON{
		"existing1": {
			Schema:      model.CurrentSchemaURL,
			Name:        "com.example/existing-server",
			Description: "An existing server",
			Version:     "1.0.0",
			Remotes: []model.Transport{
				{Type: "streamable-http", URL: "https://api.example.com/mcp"},
				{Type: "sse", URL: "https://webhook.example.com/sse"},
			},
		},
		"existing2": {
			Schema:      model.CurrentSchemaURL,
			Name:        "com.microsoft/another-server",
			Description: "Another existing server",
			Version:     "1.0.0",
			Remotes: []model.Transport{
				{Type: "streamable-http", URL: "https://api.microsoft.com/mcp"},
			},
		},
	}

	testDB := internaldb.NewTestDB(t)
	serverService := newRegistryTestServerService(testDB, &config.Config{EnableRegistryValidation: false})

	// Create existing servers using the new CreateServer method
	for _, server := range existingServers {
		_, err := serverService.PublishServer(ctx, server)
		require.NoError(t, err, "failed to create server: %v", err)
	}

	tests := []struct {
		name         string
		serverDetail apiv0.ServerJSON
		expectError  bool
		errorMsg     string
	}{
		{
			name: "no remote URLs - should pass",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/new-server",
				Description: "A new server with no remotes",
				Version:     "1.0.0",
				Remotes:     []model.Transport{},
			},
			expectError: false,
		},
		{
			name: "new unique remote URLs - should pass",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/new-server-unique",
				Description: "A new server",
				Version:     "1.0.0",
				Remotes: []model.Transport{
					{Type: "streamable-http", URL: "https://new.example.com/mcp"},
					{Type: "sse", URL: "https://unique.example.com/sse"},
				},
			},
			expectError: false,
		},
		{
			name: "duplicate remote URL - should fail",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/new-server-duplicate",
				Description: "A new server with duplicate URL",
				Version:     "1.0.0",
				Remotes: []model.Transport{
					{Type: "streamable-http", URL: "https://api.example.com/mcp"}, // This URL already exists
				},
			},
			expectError: true,
			errorMsg:    "remote URL https://api.example.com/mcp is already used by server com.example/existing-server",
		},
		{
			name: "updating same server with same URLs - should pass",
			serverDetail: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/existing-server", // Same name as existing
				Description: "Updated existing server",
				Version:     "1.1.0", // Different version
				Remotes: []model.Transport{
					{Type: "streamable-http", URL: "https://api.example.com/mcp"}, // Same URL as before
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := serverService.PublishServer(ctx, &tt.serverDetail)

			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestGetServerByName(t *testing.T) {
	ctx := context.Background()
	testDB := internaldb.NewTestDB(t)
	serverService := newRegistryTestServerService(testDB, &config.Config{EnableRegistryValidation: false})

	// Create multiple versions of the same server
	_, err := serverService.PublishServer(ctx, &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        "com.example/test-server",
		Description: "Test server v1",
		Version:     "1.0.0",
	})
	require.NoError(t, err)

	_, err = serverService.PublishServer(ctx, &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        "com.example/test-server",
		Description: "Test server v2",
		Version:     "2.0.0",
	})
	require.NoError(t, err)

	tests := []struct {
		name        string
		serverName  string
		expectError bool
		errorMsg    string
		checkResult func(*testing.T, *apiv0.ServerResponse)
	}{
		{
			name:        "get latest version by server name",
			serverName:  "com.example/test-server",
			expectError: false,
			checkResult: func(t *testing.T, result *apiv0.ServerResponse) {
				t.Helper()
				assert.Equal(t, "2.0.0", result.Server.Version) // Should get latest version
				assert.Equal(t, "Test server v2", result.Server.Description)
				assert.True(t, result.Meta.Official.IsLatest)
			},
		},
		{
			name:        "server not found",
			serverName:  "com.example/non-existent",
			expectError: true,
			errorMsg:    "record not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := serverService.GetServer(ctx, tt.serverName)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
				assert.Nil(t, result)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, result)
				if tt.checkResult != nil {
					tt.checkResult(t, result)
				}
			}
		})
	}
}

func TestGetServerByNameAndVersion(t *testing.T) {
	ctx := context.Background()
	testDB := internaldb.NewTestDB(t)
	serverService := newRegistryTestServerService(testDB, &config.Config{EnableRegistryValidation: false})

	serverName := "com.example/versioned-server"

	// Create multiple versions of the same server
	_, err := serverService.PublishServer(ctx, &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        serverName,
		Description: "Versioned server v1",
		Version:     "1.0.0",
	})
	require.NoError(t, err)

	_, err = serverService.PublishServer(ctx, &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        serverName,
		Description: "Versioned server v2",
		Version:     "2.0.0",
	})
	require.NoError(t, err)

	tests := []struct {
		name        string
		serverName  string
		version     string
		expectError bool
		errorMsg    string
		checkResult func(*testing.T, *apiv0.ServerResponse)
	}{
		{
			name:        "get specific version 1.0.0",
			serverName:  serverName,
			version:     "1.0.0",
			expectError: false,
			checkResult: func(t *testing.T, result *apiv0.ServerResponse) {
				t.Helper()
				assert.Equal(t, "1.0.0", result.Server.Version)
				assert.Equal(t, "Versioned server v1", result.Server.Description)
				assert.False(t, result.Meta.Official.IsLatest)
			},
		},
		{
			name:        "get specific version 2.0.0",
			serverName:  serverName,
			version:     "2.0.0",
			expectError: false,
			checkResult: func(t *testing.T, result *apiv0.ServerResponse) {
				t.Helper()
				assert.Equal(t, "2.0.0", result.Server.Version)
				assert.Equal(t, "Versioned server v2", result.Server.Description)
				assert.True(t, result.Meta.Official.IsLatest)
			},
		},
		{
			name:        "version not found",
			serverName:  serverName,
			version:     "3.0.0",
			expectError: true,
			errorMsg:    "record not found",
		},
		{
			name:        "server not found",
			serverName:  "com.example/non-existent",
			version:     "1.0.0",
			expectError: true,
			errorMsg:    "record not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := serverService.GetServerVersion(ctx, tt.serverName, tt.version)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
				assert.Nil(t, result)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, result)
				if tt.checkResult != nil {
					tt.checkResult(t, result)
				}
			}
		})
	}
}

func TestStoreAndRetrieveServerReadme(t *testing.T) {
	ctx := context.Background()
	testDB := internaldb.NewTestDB(t)
	serverService := newRegistryTestServerService(testDB, &config.Config{EnableRegistryValidation: false})

	serverName := "com.example/readme-server"

	_, err := serverService.PublishServer(ctx, &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        serverName,
		Description: "Readme server v1",
		Version:     "1.0.0",
	})
	require.NoError(t, err)

	firstReadme := []byte("# Version 1\nHello world\n")
	ctxWithAuth := internaldb.WithTestSession(ctx)
	require.NoError(t, serverService.SetServerReadme(ctxWithAuth, serverName, "1.0.0", firstReadme, ""))

	readmeV1, err := serverService.GetServerReadme(ctx, serverName, "1.0.0")
	require.NoError(t, err)
	require.NotNil(t, readmeV1)
	assert.Equal(t, "1.0.0", readmeV1.Version)
	assert.Equal(t, len(firstReadme), readmeV1.SizeBytes)
	assert.Equal(t, "text/markdown", readmeV1.ContentType)
	assert.Equal(t, string(firstReadme), string(readmeV1.Content))
	assert.NotEmpty(t, readmeV1.SHA256)

	latest, err := serverService.GetLatestServerReadme(ctx, serverName)
	require.NoError(t, err)
	require.NotNil(t, latest)
	assert.Equal(t, "1.0.0", latest.Version)
	assert.Equal(t, string(firstReadme), string(latest.Content))

	_, err = serverService.PublishServer(ctx, &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        serverName,
		Description: "Readme server v2",
		Version:     "2.0.0",
	})
	require.NoError(t, err)

	secondReadme := []byte("# Version 2\nUpdated\n")
	require.NoError(t, serverService.SetServerReadme(ctxWithAuth, serverName, "2.0.0", secondReadme, "text/markdown"))

	latest, err = serverService.GetLatestServerReadme(ctx, serverName)
	require.NoError(t, err)
	require.NotNil(t, latest)
	assert.Equal(t, "2.0.0", latest.Version)
	assert.Equal(t, string(secondReadme), string(latest.Content))

	readmeV1Again, err := serverService.GetServerReadme(ctx, serverName, "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, string(firstReadme), string(readmeV1Again.Content))
}

func TestGetServerReadmeMissing(t *testing.T) {
	ctx := context.Background()
	testDB := internaldb.NewTestDB(t)
	serverService := newRegistryTestServerService(testDB, &config.Config{EnableRegistryValidation: false})

	serverName := "com.example/missing-readme"

	_, err := serverService.PublishServer(ctx, &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        serverName,
		Description: "Server without readme",
		Version:     "1.0.0",
	})
	require.NoError(t, err)

	_, err = serverService.GetServerReadme(ctx, serverName, "1.0.0")
	require.Error(t, err)
	assert.Equal(t, database.ErrNotFound, err)

	_, err = serverService.GetLatestServerReadme(ctx, serverName)
	require.Error(t, err)
	assert.Equal(t, database.ErrNotFound, err)
}

func TestGetAllVersionsByServerName(t *testing.T) {
	ctx := context.Background()
	testDB := internaldb.NewTestDB(t)
	serverService := newRegistryTestServerService(testDB, &config.Config{EnableRegistryValidation: false})

	serverName := "com.example/multi-version-server"

	// Create multiple versions of the same server
	_, err := serverService.PublishServer(ctx, &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        serverName,
		Description: "Multi-version server v1",
		Version:     "1.0.0",
	})
	require.NoError(t, err)

	_, err = serverService.PublishServer(ctx, &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        serverName,
		Description: "Multi-version server v2",
		Version:     "2.0.0",
	})
	require.NoError(t, err)

	_, err = serverService.PublishServer(ctx, &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        serverName,
		Description: "Multi-version server v2.1",
		Version:     "2.1.0",
	})
	require.NoError(t, err)

	tests := []struct {
		name        string
		serverName  string
		expectError bool
		errorMsg    string
		checkResult func(*testing.T, []*apiv0.ServerResponse)
	}{
		{
			name:        "get all versions of server",
			serverName:  serverName,
			expectError: false,
			checkResult: func(t *testing.T, result []*apiv0.ServerResponse) {
				t.Helper()
				assert.Len(t, result, 3)

				// Collect versions
				versions := make([]string, 0, len(result))
				latestCount := 0
				for _, server := range result {
					versions = append(versions, server.Server.Version)
					assert.Equal(t, serverName, server.Server.Name)
					if server.Meta.Official.IsLatest {
						latestCount++
					}
				}

				// Verify all versions are present
				assert.Contains(t, versions, "1.0.0")
				assert.Contains(t, versions, "2.0.0")
				assert.Contains(t, versions, "2.1.0")

				// Only one should be marked as latest
				assert.Equal(t, 1, latestCount)
			},
		},
		{
			name:        "server not found",
			serverName:  "com.example/non-existent",
			expectError: true,
			errorMsg:    "record not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := serverService.GetServerVersions(ctx, tt.serverName)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
				assert.Empty(t, result)
			} else {
				require.NoError(t, err)
				assert.NotEmpty(t, result)
				if tt.checkResult != nil {
					tt.checkResult(t, result)
				}
			}
		})
	}
}

func TestCreateServerConcurrentVersionsNoRace(t *testing.T) {
	ctx := context.Background()
	testDB := internaldb.NewTestDB(t)
	serverService := newRegistryTestServerService(testDB, &config.Config{EnableRegistryValidation: false})

	const concurrency = 100
	serverName := "com.example/test-concurrent"
	results := make([]*apiv0.ServerResponse, concurrency)
	errors := make([]error, concurrency)

	var wg sync.WaitGroup
	for i := range concurrency {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			result, err := serverService.PublishServer(ctx, &apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        serverName,
				Description: fmt.Sprintf("Version %d", idx),
				Version:     fmt.Sprintf("1.0.%d", idx),
			})
			results[idx] = result
			errors[idx] = err
		}(i)
	}
	wg.Wait()

	// All publishes should succeed
	for i, err := range errors {
		require.NoError(t, err, "create server %d failed", i)
	}

	// All results should have the same server name
	for i, result := range results {
		if result != nil {
			assert.Equal(t, serverName, result.Server.Name, "version %d has different server name", i)
		}
	}

	// Query database to check the final state after all creates complete
	allVersions, err := serverService.GetServerVersions(ctx, serverName)
	require.NoError(t, err, "failed to get all versions")

	latestCount := 0
	var latestVersion string
	for _, r := range allVersions {
		if r.Meta.Official.IsLatest {
			latestCount++
			latestVersion = r.Server.Version
		}
	}

	assert.Equal(t, 1, latestCount, "should have exactly one latest version in database, found version: %s", latestVersion)
	assert.Len(t, allVersions, concurrency, "should have all %d versions", concurrency)
}

func TestUpdateServer(t *testing.T) {
	ctx := context.Background()
	testDB := internaldb.NewTestDB(t)
	serverService := newRegistryTestServerService(testDB, &config.Config{EnableRegistryValidation: false})

	serverName := "com.example/update-test-server"
	version := "1.0.0"

	// Create initial server
	_, err := serverService.PublishServer(ctx, &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        serverName,
		Description: "Original description",
		Version:     version,
		Remotes: []model.Transport{
			{Type: "streamable-http", URL: "https://original.example.com/mcp"},
		},
	})
	require.NoError(t, err)

	tests := []struct {
		name          string
		serverName    string
		version       string
		updatedServer *apiv0.ServerJSON
		newStatus     *string
		expectError   bool
		errorMsg      string
		checkResult   func(*testing.T, *apiv0.ServerResponse)
	}{
		{
			name:       "successful server update",
			serverName: serverName,
			version:    version,
			updatedServer: &apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        serverName,
				Description: "Updated description",
				Version:     version,
				Remotes: []model.Transport{
					{Type: "streamable-http", URL: "https://updated.example.com/mcp"},
				},
			},
			expectError: false,
			checkResult: func(t *testing.T, result *apiv0.ServerResponse) {
				t.Helper()
				assert.Equal(t, "Updated description", result.Server.Description)
				assert.Len(t, result.Server.Remotes, 1)
				assert.Equal(t, "https://updated.example.com/mcp", result.Server.Remotes[0].URL)
				assert.NotZero(t, result.Meta.Official.UpdatedAt)
			},
		},
		{
			name:       "update with status change",
			serverName: serverName,
			version:    version,
			updatedServer: &apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        serverName,
				Description: "Updated with status change",
				Version:     version,
			},
			newStatus:   stringPtr(string(model.StatusDeprecated)),
			expectError: false,
			checkResult: func(t *testing.T, result *apiv0.ServerResponse) {
				t.Helper()
				assert.Equal(t, "Updated with status change", result.Server.Description)
				assert.Equal(t, model.StatusDeprecated, result.Meta.Official.Status)
			},
		},
		{
			name:       "update non-existent server",
			serverName: "com.example/non-existent",
			version:    "1.0.0",
			updatedServer: &apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/non-existent",
				Description: "Should fail",
				Version:     "1.0.0",
			},
			expectError: true,
			errorMsg:    "record not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctxWithAuth := internaldb.WithTestSession(ctx)
			result, err := serverService.UpdateServer(ctxWithAuth, tt.serverName, tt.version, tt.updatedServer, tt.newStatus)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
				assert.Nil(t, result)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, result)
				if tt.checkResult != nil {
					tt.checkResult(t, result)
				}
			}
		})
	}
}

func TestUpdateServer_SkipValidationForDeletedServers(t *testing.T) {
	ctx := context.Background()
	testDB := internaldb.NewTestDB(t)
	// Enable registry validation to test that it gets skipped for deleted servers
	cfg := &config.Config{EnableRegistryValidation: true}
	serverService := newRegistryTestServerService(testDB, cfg)

	serverName := "com.example/validation-skip-test"
	version := "1.0.0"

	// Create server with invalid package configuration (this would fail registry validation)
	invalidServer := &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        serverName,
		Description: "Server with invalid package for testing validation skip",
		Version:     version,
		Packages: []model.Package{
			{
				RegistryType: "npm",
				Identifier:   "non-existent-package-for-validation-test",
				Version:      "1.0.0",
				Transport:    model.Transport{Type: "stdio"},
			},
		},
	}

	// Create initial server (validation disabled for creation in this test)
	originalConfig := cfg.EnableRegistryValidation
	cfg.EnableRegistryValidation = false
	_, err := serverService.PublishServer(ctx, invalidServer)
	require.NoError(t, err, "failed to create server with validation disabled")
	cfg.EnableRegistryValidation = originalConfig

	// First, set server to deleted status
	ctxWithAuth := internaldb.WithTestSession(ctx)
	deletedStatus := string(model.StatusDeleted)
	_, err = serverService.UpdateServer(ctxWithAuth, serverName, version, invalidServer, &deletedStatus)
	require.NoError(t, err, "should be able to set server to deleted (validation should be skipped)")

	// Verify server is now deleted
	updatedServer, err := serverService.GetServerVersion(ctx, serverName, version)
	require.NoError(t, err)
	assert.Equal(t, model.StatusDeleted, updatedServer.Meta.Official.Status)

	// Now try to update a deleted server - validation should be skipped
	updatedInvalidServer := &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        serverName,
		Description: "Updated description for deleted server",
		Version:     version,
		Packages: []model.Package{
			{
				RegistryType: "npm",
				Identifier:   "another-non-existent-package-for-validation-test",
				Version:      "2.0.0",
				Transport:    model.Transport{Type: "stdio"},
			},
		},
	}

	// This should succeed despite invalid packages because server is deleted
	result, err := serverService.UpdateServer(ctxWithAuth, serverName, version, updatedInvalidServer, nil)
	require.NoError(t, err, "updating deleted server should skip registry validation")
	assert.NotNil(t, result)
	assert.Equal(t, "Updated description for deleted server", result.Server.Description)
	assert.Equal(t, model.StatusDeleted, result.Meta.Official.Status)

	// Test updating a server being set to deleted status
	activeServer := &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        "com.example/being-deleted-test",
		Description: "Server being deleted",
		Version:     "1.0.0",
		Packages: []model.Package{
			{
				RegistryType: "npm",
				Identifier:   "yet-another-non-existent-package",
				Version:      "1.0.0",
				Transport:    model.Transport{Type: "stdio"},
			},
		},
	}

	// Create active server (with validation disabled)
	cfg.EnableRegistryValidation = false
	_, err = serverService.PublishServer(ctx, activeServer)
	require.NoError(t, err)
	cfg.EnableRegistryValidation = originalConfig

	// Update server and set to deleted in same operation - should skip validation
	newDeletedStatus := string(model.StatusDeleted)
	result2, err := serverService.UpdateServer(ctxWithAuth, "com.example/being-deleted-test", "1.0.0", activeServer, &newDeletedStatus)
	require.NoError(t, err, "updating server being set to deleted should skip registry validation")
	assert.NotNil(t, result2)
	assert.Equal(t, model.StatusDeleted, result2.Meta.Official.Status)
}

func TestListServers(t *testing.T) {
	ctx := context.Background()
	testDB := internaldb.NewTestDB(t)
	serverService := newRegistryTestServerService(testDB, &config.Config{EnableRegistryValidation: false})

	// Create test servers
	testServers := []struct {
		name        string
		version     string
		description string
	}{
		{"com.example/server-alpha", "1.0.0", "Alpha server"},
		{"com.example/server-beta", "1.0.0", "Beta server"},
		{"com.example/server-gamma", "2.0.0", "Gamma server"},
	}

	for _, server := range testServers {
		_, err := serverService.PublishServer(ctx, &apiv0.ServerJSON{
			Schema:      model.CurrentSchemaURL,
			Name:        server.name,
			Description: server.description,
			Version:     server.version,
		})
		require.NoError(t, err)
	}

	tests := []struct {
		name          string
		filter        *database.ServerFilter
		cursor        string
		limit         int
		expectedCount int
		expectError   bool
	}{
		{
			name:          "list all servers",
			filter:        nil,
			limit:         10,
			expectedCount: 3,
		},
		{
			name: "filter by name",
			filter: &database.ServerFilter{
				Name: stringPtr("com.example/server-alpha"),
			},
			limit:         10,
			expectedCount: 1,
		},
		{
			name: "filter by version",
			filter: &database.ServerFilter{
				Version: stringPtr("1.0.0"),
			},
			limit:         10,
			expectedCount: 2,
		},
		{
			name:          "pagination with limit",
			filter:        nil,
			limit:         2,
			expectedCount: 2,
		},
		{
			name:   "cursor pagination",
			filter: nil,
			cursor: "com.example/server-alpha",
			limit:  10,
			// Should return servers after 'server-alpha' alphabetically
			expectedCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, nextCursor, err := serverService.ListServers(ctx, tt.filter, tt.cursor, tt.limit)

			if tt.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, results, tt.expectedCount)

			// Test cursor behavior
			if tt.limit < len(testServers) && len(results) == tt.limit {
				assert.NotEmpty(t, nextCursor, "Should return next cursor when results are limited")
			}
		})
	}
}

func TestVersionComparison(t *testing.T) {
	ctx := context.Background()
	testDB := internaldb.NewTestDB(t)
	serverService := newRegistryTestServerService(testDB, &config.Config{EnableRegistryValidation: false})

	serverName := "com.example/version-comparison-server"

	// Create versions in non-chronological order to test version comparison logic
	versions := []struct {
		version     string
		description string
		delay       time.Duration // Delay to simulate different publish times
	}{
		{"2.0.0", "Version 2.0.0", 0},
		{"1.0.0", "Version 1.0.0", 10 * time.Millisecond},
		{"2.1.0", "Version 2.1.0", 20 * time.Millisecond},
		{"1.5.0", "Version 1.5.0", 30 * time.Millisecond},
	}

	for _, v := range versions {
		if v.delay > 0 {
			time.Sleep(v.delay)
		}
		_, err := serverService.PublishServer(ctx, &apiv0.ServerJSON{
			Schema:      model.CurrentSchemaURL,
			Name:        serverName,
			Description: v.description,
			Version:     v.version,
		})
		require.NoError(t, err, "Failed to create version %s", v.version)
	}

	// Get the latest version - should be 2.1.0 based on semantic versioning
	latest, err := serverService.GetServer(ctx, serverName)
	require.NoError(t, err)

	assert.Equal(t, "2.1.0", latest.Server.Version, "Latest version should be 2.1.0")
	assert.True(t, latest.Meta.Official.IsLatest)

	// Verify only one version is marked as latest
	allVersions, err := serverService.GetServerVersions(ctx, serverName)
	require.NoError(t, err)

	latestCount := 0
	for _, version := range allVersions {
		if version.Meta.Official.IsLatest {
			latestCount++
		}
	}
	assert.Equal(t, 1, latestCount, "Exactly one version should be marked as latest")
}

func TestDeployServer_AlreadyExistsDoesNotAttemptIdentityCleanup(t *testing.T) {
	ctx := context.Background()
	createCalls := 0
	getDeploymentsCalls := 0
	removeCalls := 0

	mockDB := &deployCreateMockDB{
		getProviderByIDFn: func(_ context.Context, providerID string) (*models.Provider, error) {
			return &models.Provider{ID: providerID, Platform: "local"}, nil
		},
		getServerByNameAndVersionFn: func(_ context.Context, serverName, version string) (*apiv0.ServerResponse, error) {
			return &apiv0.ServerResponse{
				Server: apiv0.ServerJSON{
					Name:    serverName,
					Version: version,
				},
			}, nil
		},
		createDeploymentFn: func(_ context.Context, _ *models.Deployment) error {
			createCalls++
			return database.ErrAlreadyExists
		},
		getDeploymentsFn: func(_ context.Context, _ *models.DeploymentFilter) ([]*models.Deployment, error) {
			getDeploymentsCalls++
			return []*models.Deployment{}, nil
		},
		removeDeploymentByIDFn: func(_ context.Context, _ string) error {
			removeCalls++
			return nil
		},
	}

	svc := &registryServiceImpl{
		storeDB: mockDB,
		deploymentAdapters: map[string]registrytypes.DeploymentPlatformAdapter{
			"local": &testDeploymentAdapter{},
		},
	}
	_, err := svc.DeployServer(ctx, "com.example/weather", "1.0.0", map[string]string{}, false, "local")
	require.ErrorIs(t, err, database.ErrAlreadyExists)
	assert.Equal(t, 1, createCalls)
	assert.Equal(t, 0, getDeploymentsCalls)
	assert.Equal(t, 0, removeCalls)
}

func TestDeployAgent_AlreadyExistsDoesNotAttemptIdentityCleanup(t *testing.T) {
	ctx := context.Background()
	createCalls := 0
	getDeploymentsCalls := 0
	removeCalls := 0

	mockDB := &deployCreateMockDB{
		getProviderByIDFn: func(_ context.Context, providerID string) (*models.Provider, error) {
			return &models.Provider{ID: providerID, Platform: "local"}, nil
		},
		getAgentByNameAndVersionFn: func(_ context.Context, agentName, version string) (*models.AgentResponse, error) {
			return &models.AgentResponse{
				Agent: models.AgentJSON{
					AgentManifest: models.AgentManifest{Name: agentName},
					Version:       version,
				},
			}, nil
		},
		createDeploymentFn: func(_ context.Context, _ *models.Deployment) error {
			createCalls++
			return database.ErrAlreadyExists
		},
		getDeploymentsFn: func(_ context.Context, _ *models.DeploymentFilter) ([]*models.Deployment, error) {
			getDeploymentsCalls++
			return []*models.Deployment{}, nil
		},
		removeDeploymentByIDFn: func(_ context.Context, _ string) error {
			removeCalls++
			return nil
		},
	}

	svc := &registryServiceImpl{
		storeDB: mockDB,
		deploymentAdapters: map[string]registrytypes.DeploymentPlatformAdapter{
			"local": &testDeploymentAdapter{},
		},
	}
	_, err := svc.DeployAgent(ctx, "com.example/planner", "1.0.0", map[string]string{}, false, "local")
	require.ErrorIs(t, err, database.ErrAlreadyExists)
	assert.Equal(t, 1, createCalls)
	assert.Equal(t, 0, getDeploymentsCalls)
	assert.Equal(t, 0, removeCalls)
}

func TestDeployServer_MissingProviderIDReturnsInvalidInput(t *testing.T) {
	svc := &registryServiceImpl{}
	_, err := svc.DeployServer(context.Background(), "com.example/weather", "1.0.0", map[string]string{}, false, "")
	require.ErrorIs(t, err, database.ErrInvalidInput)
}

func TestDeployAgent_MissingProviderIDReturnsInvalidInput(t *testing.T) {
	svc := &registryServiceImpl{}
	_, err := svc.DeployAgent(context.Background(), "com.example/planner", "1.0.0", map[string]string{}, false, "")
	require.ErrorIs(t, err, database.ErrInvalidInput)
}

func TestLaunchDeployment_MissingProviderIDReturnsInvalidInput(t *testing.T) {
	svc := &registryServiceImpl{}
	_, err := svc.LaunchDeployment(context.Background(), &models.Deployment{
		ServerName:   "com.example/weather",
		Version:      "1.0.0",
		ResourceType: "mcp",
	})
	require.ErrorIs(t, err, database.ErrInvalidInput)
}

func TestCreateManagedDeploymentRecord_UsesDeployingStatus(t *testing.T) {
	ctx := context.Background()
	var createdRecord *models.Deployment

	mockDB := &deployCreateMockDB{
		getServerByNameAndVersionFn: func(_ context.Context, serverName, version string) (*apiv0.ServerResponse, error) {
			return &apiv0.ServerResponse{
				Server: apiv0.ServerJSON{
					Name:    serverName,
					Version: version,
				},
			}, nil
		},
		createDeploymentFn: func(_ context.Context, deployment *models.Deployment) error {
			clonedEnv := map[string]string{}
			maps.Copy(clonedEnv, deployment.Env)
			createdRecord = &models.Deployment{
				ID:           deployment.ID,
				ServerName:   deployment.ServerName,
				Version:      deployment.Version,
				Status:       deployment.Status,
				ResourceType: deployment.ResourceType,
				ProviderID:   deployment.ProviderID,
				Origin:       deployment.Origin,
				Env:          clonedEnv,
			}
			return nil
		},
		getDeploymentByIDFn: func(_ context.Context, _ string) (*models.Deployment, error) {
			return createdRecord, nil
		},
	}

	svc := newDeploymentInternals(deploymentsvc.Dependencies{
		Deployments: mockDB.Deployments(),
		Servers:     serversvc.New(serversvc.Dependencies{Servers: mockDB.Servers(), Tx: mockDB}),
		Agents:      agentsvc.New(agentsvc.Dependencies{Agents: mockDB.Agents(), Tx: mockDB}),
	})

	created, err := svc.CreateManagedDeploymentRecord(ctx, &models.Deployment{
		ID:           "dep-create-1",
		ServerName:   "com.example/weather",
		Version:      "1.0.0",
		ResourceType: "mcp",
		ProviderID:   "kubernetes-default",
		Origin:       "managed",
	})
	require.NoError(t, err)
	require.NotNil(t, createdRecord)
	assert.Equal(t, "deploying", createdRecord.Status)
	require.NotNil(t, created)
	assert.Equal(t, "deploying", created.Status)
}

func TestApplyDeploymentActionResult_UsesSystemContext(t *testing.T) {
	ctx := context.Background()
	mockDB := &deployCreateMockDB{
		updateDeploymentStateFn: func(ctx context.Context, id string, patch *models.DeploymentStatePatch) error {
			session, ok := auth.AuthSessionFrom(ctx)
			require.True(t, ok)
			require.True(t, auth.IsSystemSession(session))
			require.Equal(t, "dep-1", id)
			require.NotNil(t, patch)
			require.NotNil(t, patch.Status)
			require.Equal(t, "deployed", *patch.Status)
			require.NotNil(t, patch.Error)
			require.Empty(t, *patch.Error)
			return nil
		},
	}

	svc := newDeploymentInternals(deploymentsvc.Dependencies{Deployments: mockDB.Deployments()})
	err := svc.ApplyDeploymentActionResult(ctx, "dep-1", &models.DeploymentActionResult{Status: "deployed"})
	require.NoError(t, err)
}

func TestApplyFailedDeploymentAction_UsesSystemContext(t *testing.T) {
	ctx := context.Background()
	mockDB := &deployCreateMockDB{
		updateDeploymentStateFn: func(ctx context.Context, id string, patch *models.DeploymentStatePatch) error {
			session, ok := auth.AuthSessionFrom(ctx)
			require.True(t, ok)
			require.True(t, auth.IsSystemSession(session))
			require.Equal(t, "dep-2", id)
			require.NotNil(t, patch)
			require.NotNil(t, patch.Status)
			require.Equal(t, "failed", *patch.Status)
			require.NotNil(t, patch.Error)
			require.Equal(t, "boom", *patch.Error)
			return nil
		},
	}

	svc := newDeploymentInternals(deploymentsvc.Dependencies{Deployments: mockDB.Deployments()})
	err := svc.ApplyFailedDeploymentAction(ctx, "dep-2", fmt.Errorf("boom"), nil)
	require.NoError(t, err)
}

type deployCreateMockDB struct {
	database.Store
	getProviderByIDFn           func(ctx context.Context, providerID string) (*models.Provider, error)
	getServerByNameAndVersionFn func(ctx context.Context, serverName, version string) (*apiv0.ServerResponse, error)
	getAgentByNameAndVersionFn  func(ctx context.Context, agentName, version string) (*models.AgentResponse, error)
	createDeploymentFn          func(ctx context.Context, deployment *models.Deployment) error
	getDeploymentByIDFn         func(ctx context.Context, id string) (*models.Deployment, error)
	updateDeploymentStateFn     func(ctx context.Context, id string, patch *models.DeploymentStatePatch) error
	getDeploymentsFn            func(ctx context.Context, filter *models.DeploymentFilter) ([]*models.Deployment, error)
	removeDeploymentByIDFn      func(ctx context.Context, id string) error
}

func (m *deployCreateMockDB) Servers() database.ServerStore {
	return m
}

func (m *deployCreateMockDB) Agents() database.AgentStore {
	return m
}

func (m *deployCreateMockDB) Providers() database.ProviderStore {
	return m
}

func (m *deployCreateMockDB) Deployments() database.DeploymentStore {
	return m
}

// deploymentMockDB is a minimal mock for database.Store that only implements
// the methods needed for testing deployment cleanup logic. All other methods panic.
type deploymentMockDB struct {
	database.Store         // embed interface so unimplemented methods panic
	getDeploymentByIDFn    func(ctx context.Context, id string) (*models.Deployment, error)
	getDeploymentsFn       func(ctx context.Context, filter *models.DeploymentFilter) ([]*models.Deployment, error)
	listProvidersFn        func(ctx context.Context, platform *string) ([]*models.Provider, error)
	getProviderByIDFn      func(ctx context.Context, providerID string) (*models.Provider, error)
	removeDeploymentByIdFn func(ctx context.Context, id string) error
}

func (m *deploymentMockDB) Providers() database.ProviderStore {
	return m
}

func (m *deploymentMockDB) Deployments() database.DeploymentStore {
	return m
}

func (m *deployCreateMockDB) GetProvider(ctx context.Context, providerID string) (*models.Provider, error) {
	return m.getProviderByIDFn(ctx, providerID)
}

func (m *deployCreateMockDB) CreateProvider(context.Context, *models.CreateProviderInput) (*models.Provider, error) {
	return nil, database.ErrInvalidInput
}

func (m *deployCreateMockDB) ListProviders(context.Context, *string) ([]*models.Provider, error) {
	return nil, nil
}

func (m *deployCreateMockDB) UpdateProvider(context.Context, string, *models.UpdateProviderInput) (*models.Provider, error) {
	return nil, database.ErrInvalidInput
}

func (m *deployCreateMockDB) DeleteProvider(context.Context, string) error {
	return nil
}

func (m *deployCreateMockDB) DeleteServer(context.Context, string, string) error {
	return nil
}

func (m *deployCreateMockDB) CreateServer(context.Context, *apiv0.ServerJSON, *apiv0.RegistryExtensions) (*apiv0.ServerResponse, error) {
	return nil, database.ErrInvalidInput
}

func (m *deployCreateMockDB) UpdateServer(context.Context, string, string, *apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
	return nil, database.ErrInvalidInput
}

func (m *deployCreateMockDB) SetServerStatus(context.Context, string, string, string) (*apiv0.ServerResponse, error) {
	return nil, database.ErrInvalidInput
}

func (m *deployCreateMockDB) ListServers(context.Context, *database.ServerFilter, string, int) ([]*apiv0.ServerResponse, string, error) {
	return nil, "", nil
}

func (m *deployCreateMockDB) GetServerVersion(ctx context.Context, serverName, version string) (*apiv0.ServerResponse, error) {
	return m.getServerByNameAndVersionFn(ctx, serverName, version)
}

func (m *deployCreateMockDB) GetServer(context.Context, string) (*apiv0.ServerResponse, error) {
	return nil, database.ErrNotFound
}

func (m *deployCreateMockDB) GetServerVersions(context.Context, string) ([]*apiv0.ServerResponse, error) {
	return nil, database.ErrNotFound
}

func (m *deployCreateMockDB) GetLatestServer(context.Context, string) (*apiv0.ServerResponse, error) {
	return nil, database.ErrNotFound
}

func (m *deployCreateMockDB) CountServerVersions(context.Context, string) (int, error) {
	return 0, nil
}

func (m *deployCreateMockDB) CheckVersionExists(context.Context, string, string) (bool, error) {
	return false, nil
}

func (m *deployCreateMockDB) UnmarkAsLatest(context.Context, string) error {
	return nil
}

func (m *deployCreateMockDB) AcquireServerCreateLock(context.Context, string) error {
	return nil
}

func (m *deployCreateMockDB) SetServerEmbedding(context.Context, string, string, *database.SemanticEmbedding) error {
	return nil
}

func (m *deployCreateMockDB) GetServerEmbeddingMetadata(context.Context, string, string) (*database.SemanticEmbeddingMetadata, error) {
	return nil, database.ErrNotFound
}

func (m *deployCreateMockDB) UpsertServerReadme(context.Context, *database.ServerReadme) error {
	return nil
}

func (m *deployCreateMockDB) GetServerReadme(context.Context, string, string) (*database.ServerReadme, error) {
	return nil, database.ErrNotFound
}

func (m *deployCreateMockDB) GetLatestServerReadme(context.Context, string) (*database.ServerReadme, error) {
	return nil, database.ErrNotFound
}

func (m *deployCreateMockDB) GetAgentVersion(ctx context.Context, agentName, version string) (*models.AgentResponse, error) {
	return m.getAgentByNameAndVersionFn(ctx, agentName, version)
}

func (m *deployCreateMockDB) CreateAgent(context.Context, *models.AgentJSON, *models.AgentRegistryExtensions) (*models.AgentResponse, error) {
	return nil, database.ErrInvalidInput
}

func (m *deployCreateMockDB) UpdateAgent(context.Context, string, string, *models.AgentJSON) (*models.AgentResponse, error) {
	return nil, database.ErrInvalidInput
}

func (m *deployCreateMockDB) SetAgentStatus(context.Context, string, string, string) (*models.AgentResponse, error) {
	return nil, database.ErrInvalidInput
}

func (m *deployCreateMockDB) ListAgents(context.Context, *database.AgentFilter, string, int) ([]*models.AgentResponse, string, error) {
	return nil, "", nil
}

func (m *deployCreateMockDB) GetAgent(context.Context, string) (*models.AgentResponse, error) {
	return nil, database.ErrNotFound
}

func (m *deployCreateMockDB) GetAgentVersions(context.Context, string) ([]*models.AgentResponse, error) {
	return nil, database.ErrNotFound
}

func (m *deployCreateMockDB) GetLatestAgent(context.Context, string) (*models.AgentResponse, error) {
	return nil, database.ErrNotFound
}

func (m *deployCreateMockDB) CountAgentVersions(context.Context, string) (int, error) {
	return 0, nil
}

func (m *deployCreateMockDB) CheckAgentVersionExists(context.Context, string, string) (bool, error) {
	return false, nil
}

func (m *deployCreateMockDB) UnmarkAgentAsLatest(context.Context, string) error {
	return nil
}

func (m *deployCreateMockDB) DeleteAgent(context.Context, string, string) error {
	return nil
}

func (m *deployCreateMockDB) SetAgentEmbedding(context.Context, string, string, *database.SemanticEmbedding) error {
	return nil
}

func (m *deployCreateMockDB) GetAgentEmbeddingMetadata(context.Context, string, string) (*database.SemanticEmbeddingMetadata, error) {
	return nil, database.ErrNotFound
}

func (m *deployCreateMockDB) CreateDeployment(ctx context.Context, deployment *models.Deployment) error {
	return m.createDeploymentFn(ctx, deployment)
}

func (m *deployCreateMockDB) GetDeployment(ctx context.Context, id string) (*models.Deployment, error) {
	return m.getDeploymentByIDFn(ctx, id)
}

func (m *deployCreateMockDB) UpdateDeploymentState(ctx context.Context, id string, patch *models.DeploymentStatePatch) error {
	return m.updateDeploymentStateFn(ctx, id, patch)
}

func (m *deployCreateMockDB) ListDeployments(ctx context.Context, filter *models.DeploymentFilter) ([]*models.Deployment, error) {
	return m.getDeploymentsFn(ctx, filter)
}

func (m *deployCreateMockDB) DeleteDeployment(ctx context.Context, id string) error {
	return m.removeDeploymentByIDFn(ctx, id)
}

func (m *deploymentMockDB) ListProviders(ctx context.Context, platform *string) ([]*models.Provider, error) {
	return m.listProvidersFn(ctx, platform)
}

func (m *deploymentMockDB) CreateProvider(context.Context, *models.CreateProviderInput) (*models.Provider, error) {
	return nil, database.ErrInvalidInput
}

func (m *deploymentMockDB) UpdateProvider(context.Context, string, *models.UpdateProviderInput) (*models.Provider, error) {
	return nil, database.ErrInvalidInput
}

func (m *deploymentMockDB) DeleteProvider(context.Context, string) error {
	return nil
}

func (m *deploymentMockDB) GetDeployment(ctx context.Context, id string) (*models.Deployment, error) {
	return m.getDeploymentByIDFn(ctx, id)
}

func (m *deploymentMockDB) CreateDeployment(context.Context, *models.Deployment) error {
	return database.ErrInvalidInput
}

func (m *deploymentMockDB) ListDeployments(ctx context.Context, filter *models.DeploymentFilter) ([]*models.Deployment, error) {
	return m.getDeploymentsFn(ctx, filter)
}

func (m *deploymentMockDB) GetProvider(ctx context.Context, providerID string) (*models.Provider, error) {
	return m.getProviderByIDFn(ctx, providerID)
}

func (m *deploymentMockDB) UpdateDeploymentState(context.Context, string, *models.DeploymentStatePatch) error {
	return nil
}

func (m *deploymentMockDB) DeleteDeployment(ctx context.Context, id string) error {
	return m.removeDeploymentByIdFn(ctx, id)
}

// deploymentInternals is a test-only interface that exposes unexported-via-interface
// methods on the deployment service's concrete type, used by tests that need to
// exercise internal deployment logic directly.
type deploymentInternals interface {
	deploymentsvc.Registry
	ResolveDeploymentAdapter(platform string) (registrytypes.DeploymentPlatformAdapter, error)
	CleanupExistingDeployment(ctx context.Context, resourceName, version, resourceType string) error
	CreateManagedDeploymentRecord(ctx context.Context, req *models.Deployment) (*models.Deployment, error)
	ApplyDeploymentActionResult(ctx context.Context, deploymentID string, result *models.DeploymentActionResult) error
	ApplyFailedDeploymentAction(ctx context.Context, deploymentID string, deployErr error, result *models.DeploymentActionResult) error
}

// newDeploymentInternals constructs a deployment service from the given Dependencies,
// then asserts that its concrete type satisfies deploymentInternals.
// Callers must supply explicit Deployments/Providers/Servers/Agents rather than relying
// on StoreDB auto-expansion, since the mock DBs used in tests embed a nil database.Store
// and would panic if their unimplemented Scope methods were called.
func newDeploymentInternals(deps deploymentsvc.Dependencies) deploymentInternals {
	svc := deploymentsvc.New(deps)
	internals, ok := svc.(deploymentInternals)
	if !ok {
		panic("deployment service does not implement deploymentInternals")
	}
	return internals
}

// Helper functions
func stringPtr(s string) *string {
	return &s
}

func TestIsUnsupportedDeploymentPlatformError(t *testing.T) {
	baseErr := &deploymentsvc.UnsupportedDeploymentPlatformError{Platform: "acme"}
	wrappedErr := fmt.Errorf("wrapped: %w", baseErr)

	assert.True(t, deploymentsvc.IsUnsupportedDeploymentPlatformError(baseErr))
	assert.True(t, deploymentsvc.IsUnsupportedDeploymentPlatformError(wrappedErr))
	require.ErrorIs(t, baseErr, database.ErrInvalidInput)
	require.ErrorIs(t, wrappedErr, database.ErrInvalidInput)
	assert.False(t, deploymentsvc.IsUnsupportedDeploymentPlatformError(database.ErrInvalidInput))
}

func TestResolveDeploymentAdapter_UnsupportedPlatformReturnsTypedError(t *testing.T) {
	svc := newDeploymentInternals(deploymentsvc.Dependencies{
		DeploymentAdapters: map[string]registrytypes.DeploymentPlatformAdapter{},
	})

	_, err := svc.ResolveDeploymentAdapter("unknown-platform")
	require.Error(t, err)
	assert.True(t, deploymentsvc.IsUnsupportedDeploymentPlatformError(err))
	assert.ErrorIs(t, err, database.ErrInvalidInput)
}

type testDeploymentAdapter struct {
	deployFn       func(ctx context.Context, req *models.Deployment) (*models.DeploymentActionResult, error)
	undeployFn     func(ctx context.Context, deployment *models.Deployment) error
	getLogsFn      func(ctx context.Context, deployment *models.Deployment) ([]string, error)
	cancelFn       func(ctx context.Context, deployment *models.Deployment) error
	discoverFn     func(ctx context.Context, providerID string) ([]*models.Deployment, error)
	cleanupStaleFn func(ctx context.Context, deployment *models.Deployment) error
	supportedTypes []string
}

func (a *testDeploymentAdapter) Platform() string { return "test" }

func (a *testDeploymentAdapter) SupportedResourceTypes() []string {
	if len(a.supportedTypes) > 0 {
		return a.supportedTypes
	}
	return []string{"mcp", "agent"}
}

func (a *testDeploymentAdapter) Deploy(ctx context.Context, req *models.Deployment) (*models.DeploymentActionResult, error) {
	if a.deployFn == nil {
		return &models.DeploymentActionResult{Status: "deployed"}, nil
	}
	return a.deployFn(ctx, req)
}

func (a *testDeploymentAdapter) Undeploy(ctx context.Context, deployment *models.Deployment) error {
	if a.undeployFn == nil {
		return nil
	}
	return a.undeployFn(ctx, deployment)
}

func (a *testDeploymentAdapter) GetLogs(ctx context.Context, deployment *models.Deployment) ([]string, error) {
	if a.getLogsFn == nil {
		return []string{}, nil
	}
	return a.getLogsFn(ctx, deployment)
}

func (a *testDeploymentAdapter) Cancel(ctx context.Context, deployment *models.Deployment) error {
	if a.cancelFn == nil {
		return nil
	}
	return a.cancelFn(ctx, deployment)
}

func (a *testDeploymentAdapter) Discover(ctx context.Context, providerID string) ([]*models.Deployment, error) {
	if a.discoverFn == nil {
		return []*models.Deployment{}, nil
	}
	return a.discoverFn(ctx, providerID)
}

func (a *testDeploymentAdapter) CleanupStale(ctx context.Context, deployment *models.Deployment) error {
	if a.cleanupStaleFn == nil {
		return nil
	}
	return a.cleanupStaleFn(ctx, deployment)
}

func TestCleanupExistingDeployment_UsesAdapterStaleCleanerWhenAvailable(t *testing.T) {
	ctx := context.Background()
	cleanupCalled := false
	removeCalled := false

	mockDB := &deploymentMockDB{
		getDeploymentsFn: func(_ context.Context, _ *models.DeploymentFilter) ([]*models.Deployment, error) {
			return []*models.Deployment{
				{
					ID:           "dep-cleanup-1",
					ServerName:   "com.example/test",
					Version:      "1.0.0",
					ResourceType: "mcp",
					ProviderID:   "local",
				},
			}, nil
		},
		getProviderByIDFn: func(_ context.Context, providerID string) (*models.Provider, error) {
			return &models.Provider{ID: providerID, Platform: "local"}, nil
		},
		removeDeploymentByIdFn: func(_ context.Context, _ string) error {
			removeCalled = true
			return nil
		},
	}

	adapter := &testDeploymentAdapter{
		cleanupStaleFn: func(_ context.Context, deployment *models.Deployment) error {
			cleanupCalled = deployment != nil && deployment.ID == "dep-cleanup-1"
			return nil
		},
	}

	svc := newDeploymentInternals(deploymentsvc.Dependencies{
		Deployments: mockDB.Deployments(),
		Providers:   providersvc.New(providersvc.Dependencies{Providers: mockDB.Providers()}),
		DeploymentAdapters: map[string]registrytypes.DeploymentPlatformAdapter{
			"local": adapter,
		},
	})

	err := svc.CleanupExistingDeployment(ctx, "com.example/test", "1.0.0", "mcp")
	require.NoError(t, err)
	assert.True(t, cleanupCalled)
	assert.True(t, removeCalled, "db record should still be removed after adapter stale cleanup")
}

func TestUndeployDeployment_UsesAdapterForLocalPlatform(t *testing.T) {
	undeployCalled := false
	removeCalled := false
	mockDB := &deploymentMockDB{
		getProviderByIDFn: func(_ context.Context, providerID string) (*models.Provider, error) {
			return &models.Provider{ID: providerID, Platform: "local"}, nil
		},
		removeDeploymentByIdFn: func(_ context.Context, id string) error {
			removeCalled = id == "dep-local-1"
			return nil
		},
	}
	adapter := &testDeploymentAdapter{
		undeployFn: func(_ context.Context, deployment *models.Deployment) error {
			undeployCalled = deployment != nil && deployment.ID == "dep-local-1"
			return nil
		},
	}

	svc := &registryServiceImpl{
		storeDB: mockDB,
		deploymentAdapters: map[string]registrytypes.DeploymentPlatformAdapter{
			"local": adapter,
		},
	}

	err := svc.UndeployDeployment(context.Background(), &models.Deployment{ID: "dep-local-1", ProviderID: "local"})
	require.NoError(t, err)
	assert.True(t, undeployCalled)
	assert.True(t, removeCalled)
}

func TestUndeployDeployment_FailedOrCancelledRunsAdapterCleanup(t *testing.T) {
	tests := []struct {
		name   string
		status string
	}{
		{name: "failed", status: models.DeploymentStatusFailed},
		{name: "cancelled", status: models.DeploymentStatusCancelled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			undeployCalled := false
			removeCalled := false
			mockDB := &deploymentMockDB{
				getProviderByIDFn: func(_ context.Context, providerID string) (*models.Provider, error) {
					return &models.Provider{ID: providerID, Platform: "local"}, nil
				},
				removeDeploymentByIdFn: func(_ context.Context, id string) error {
					removeCalled = id == "dep-failed-1"
					return nil
				},
			}
			adapter := &testDeploymentAdapter{
				undeployFn: func(_ context.Context, _ *models.Deployment) error {
					undeployCalled = true
					return nil
				},
			}

			svc := &registryServiceImpl{
				storeDB: mockDB,
				deploymentAdapters: map[string]registrytypes.DeploymentPlatformAdapter{
					"local": adapter,
				},
			}

			err := svc.UndeployDeployment(context.Background(), &models.Deployment{
				ID:         "dep-failed-1",
				ProviderID: "local",
				Status:     tt.status,
			})
			require.NoError(t, err)
			assert.True(t, undeployCalled)
			assert.True(t, removeCalled)
		})
	}
}

func TestCreateDeployment_RejectsUnsupportedResourceTypeForProvider(t *testing.T) {
	mockDB := &deployCreateMockDB{
		getProviderByIDFn: func(_ context.Context, providerID string) (*models.Provider, error) {
			return &models.Provider{ID: providerID, Platform: "local"}, nil
		},
	}

	svc := &registryServiceImpl{
		storeDB: mockDB,
		deploymentAdapters: map[string]registrytypes.DeploymentPlatformAdapter{
			"local": &testDeploymentAdapter{supportedTypes: []string{"mcp"}},
		},
	}

	_, err := svc.LaunchDeployment(context.Background(), &models.Deployment{
		ServerName:   "io.test/agent",
		Version:      "1.0.0",
		ProviderID:   "local",
		ResourceType: "agent",
	})
	require.Error(t, err)
	require.ErrorIs(t, err, database.ErrInvalidInput)
	assert.Contains(t, err.Error(), `provider does not support resource type "agent"`)
}

func TestCreateDeployment_UsesAdapterResolvedFromProviderPlatform(t *testing.T) {
	tests := []struct {
		name         string
		providerID   string
		platform     string
		resourceType string
	}{
		{
			name:         "aws agent deployment",
			providerID:   "aws-prod",
			platform:     "aws",
			resourceType: "agent",
		},
		{
			name:         "gcp mcp deployment",
			providerID:   "gcp-prod",
			platform:     "gcp",
			resourceType: "mcp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var createdRecord *models.Deployment
			adapterCalled := false

			mockDB := &deployCreateMockDB{
				getProviderByIDFn: func(_ context.Context, providerID string) (*models.Provider, error) {
					require.Equal(t, tt.providerID, providerID)
					return &models.Provider{ID: providerID, Platform: tt.platform}, nil
				},
				getServerByNameAndVersionFn: func(_ context.Context, serverName, version string) (*apiv0.ServerResponse, error) {
					if tt.resourceType != "mcp" {
						t.Fatalf("unexpected server lookup for resource type %s", tt.resourceType)
					}
					return &apiv0.ServerResponse{
						Server: apiv0.ServerJSON{Name: serverName, Version: version},
					}, nil
				},
				getAgentByNameAndVersionFn: func(_ context.Context, agentName, version string) (*models.AgentResponse, error) {
					if tt.resourceType != "agent" {
						t.Fatalf("unexpected agent lookup for resource type %s", tt.resourceType)
					}
					return &models.AgentResponse{
						Agent: models.AgentJSON{
							AgentManifest: models.AgentManifest{Name: agentName},
							Version:       version,
						},
					}, nil
				},
				createDeploymentFn: func(_ context.Context, deployment *models.Deployment) error {
					cloned := *deployment
					createdRecord = &cloned
					return nil
				},
				updateDeploymentStateFn: func(_ context.Context, id string, patch *models.DeploymentStatePatch) error {
					require.NotNil(t, createdRecord)
					require.Equal(t, createdRecord.ID, id)
					if patch.Status != nil {
						createdRecord.Status = *patch.Status
					}
					return nil
				},
				getDeploymentByIDFn: func(_ context.Context, id string) (*models.Deployment, error) {
					require.NotNil(t, createdRecord)
					require.Equal(t, createdRecord.ID, id)
					return createdRecord, nil
				},
			}

			adapter := &testDeploymentAdapter{
				supportedTypes: []string{tt.resourceType},
				deployFn: func(_ context.Context, req *models.Deployment) (*models.DeploymentActionResult, error) {
					adapterCalled = true
					require.Equal(t, tt.providerID, req.ProviderID)
					require.Equal(t, tt.resourceType, req.ResourceType)
					return &models.DeploymentActionResult{Status: "deployed"}, nil
				},
			}

			svc := &registryServiceImpl{
				storeDB: mockDB,
				deploymentAdapters: map[string]registrytypes.DeploymentPlatformAdapter{
					tt.platform: adapter,
				},
			}

			got, err := svc.LaunchDeployment(context.Background(), &models.Deployment{
				ServerName:   "com.example/runtime",
				Version:      "latest",
				ResourceType: tt.resourceType,
				ProviderID:   tt.providerID,
			})
			require.NoError(t, err)
			require.True(t, adapterCalled)
			require.NotNil(t, got)
			assert.Equal(t, tt.providerID, got.ProviderID)
			assert.Equal(t, "deployed", got.Status)
		})
	}
}

func TestGetDeployments_AppendsDiscoveredDeploymentsFromAdapters(t *testing.T) {
	discoverCalled := false
	mockDB := &deploymentMockDB{
		getDeploymentsFn: func(_ context.Context, _ *models.DeploymentFilter) ([]*models.Deployment, error) {
			return []*models.Deployment{
				{
					ID:           "dep-managed-1",
					ServerName:   "io.test/managed",
					Version:      "1.0.0",
					ResourceType: "mcp",
					ProviderID:   "local",
					Origin:       "managed",
				},
			}, nil
		},
		listProvidersFn: func(_ context.Context, _ *string) ([]*models.Provider, error) {
			return []*models.Provider{
				{ID: "kubernetes-default", Platform: "kubernetes"},
			}, nil
		},
	}

	adapter := &testDeploymentAdapter{
		discoverFn: func(_ context.Context, providerID string) ([]*models.Deployment, error) {
			discoverCalled = true
			return []*models.Deployment{
				{
					ServerName:   "io.test/external",
					Version:      "unknown",
					ResourceType: "mcp",
					Status:       "deployed",
					Origin:       "discovered",
					ProviderID:   providerID,
				},
			}, nil
		},
	}

	svc := &registryServiceImpl{
		storeDB: mockDB,
		deploymentAdapters: map[string]registrytypes.DeploymentPlatformAdapter{
			"kubernetes": adapter,
		},
	}

	got, err := svc.ListDeployments(context.Background(), nil)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.True(t, discoverCalled)
	assert.Equal(t, "dep-managed-1", got[0].ID)
	assert.Equal(t, "discovered", got[1].Origin)
	assert.Equal(t, "kubernetes-default", got[1].ProviderID)
}

func TestGetDeployments_DedupesDiscoveredDeploymentsByIdentity(t *testing.T) {
	mockDB := &deploymentMockDB{
		getDeploymentsFn: func(_ context.Context, _ *models.DeploymentFilter) ([]*models.Deployment, error) {
			return []*models.Deployment{}, nil
		},
		listProvidersFn: func(_ context.Context, _ *string) ([]*models.Provider, error) {
			return []*models.Provider{
				{ID: "kubernetes-default", Platform: "kubernetes"},
			}, nil
		},
	}
	adapter := &testDeploymentAdapter{
		discoverFn: func(_ context.Context, providerID string) ([]*models.Deployment, error) {
			return []*models.Deployment{
				{
					ServerName:   "io.test/external",
					Version:      "unknown",
					ResourceType: "mcp",
					Status:       "deployed",
					Origin:       "discovered",
					ProviderID:   providerID,
				},
				{
					ServerName:   "io.test/external",
					Version:      "unknown",
					ResourceType: "mcp",
					Status:       "deployed",
					Origin:       "discovered",
					ProviderID:   providerID,
				},
			}, nil
		},
	}

	svc := &registryServiceImpl{
		storeDB: mockDB,
		deploymentAdapters: map[string]registrytypes.DeploymentPlatformAdapter{
			"kubernetes": adapter,
		},
	}

	got, err := svc.ListDeployments(context.Background(), nil)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "discovered", got[0].Origin)
	assert.NotEmpty(t, got[0].ID)
}

func TestGetDeployments_KeepsDiscoveredDeploymentsDistinctAcrossNamespaces(t *testing.T) {
	mockDB := &deploymentMockDB{
		getDeploymentByIDFn: func(_ context.Context, _ string) (*models.Deployment, error) {
			return nil, database.ErrNotFound
		},
		getDeploymentsFn: func(_ context.Context, _ *models.DeploymentFilter) ([]*models.Deployment, error) {
			return []*models.Deployment{}, nil
		},
		listProvidersFn: func(_ context.Context, _ *string) ([]*models.Provider, error) {
			return []*models.Provider{
				{ID: "kubernetes-default", Platform: "kubernetes"},
			}, nil
		},
	}
	adapter := &testDeploymentAdapter{
		discoverFn: func(_ context.Context, providerID string) ([]*models.Deployment, error) {
			metaA, err := models.UnmarshalFrom(models.KubernetesProviderMetadata{IsExternal: true, Namespace: "team-a"})
			require.NoError(t, err)
			metaB, err := models.UnmarshalFrom(models.KubernetesProviderMetadata{IsExternal: true, Namespace: "team-b"})
			require.NoError(t, err)
			return []*models.Deployment{
				{
					ServerName:       "io.test/external",
					Version:          "unknown",
					ResourceType:     "mcp",
					Status:           "deployed",
					Origin:           "discovered",
					ProviderID:       providerID,
					ProviderMetadata: metaA,
				},
				{
					ServerName:       "io.test/external",
					Version:          "unknown",
					ResourceType:     "mcp",
					Status:           "deployed",
					Origin:           "discovered",
					ProviderID:       providerID,
					ProviderMetadata: metaB,
				},
			}, nil
		},
	}

	svc := &registryServiceImpl{
		storeDB: mockDB,
		deploymentAdapters: map[string]registrytypes.DeploymentPlatformAdapter{
			"kubernetes": adapter,
		},
	}

	got, err := svc.ListDeployments(context.Background(), nil)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.NotEqual(t, got[0].ID, got[1].ID)
}

func TestGetDeployments_ManagedOriginSkipsDiscovery(t *testing.T) {
	discoverCalled := false
	originManaged := "managed"
	mockDB := &deploymentMockDB{
		getDeploymentsFn: func(_ context.Context, filter *models.DeploymentFilter) ([]*models.Deployment, error) {
			require.NotNil(t, filter)
			require.NotNil(t, filter.Origin)
			require.Equal(t, originManaged, *filter.Origin)
			return []*models.Deployment{
				{
					ID:           "dep-managed-only",
					ServerName:   "io.test/managed",
					Version:      "1.0.0",
					ResourceType: "mcp",
					ProviderID:   "local",
					Origin:       "managed",
				},
			}, nil
		},
		listProvidersFn: func(_ context.Context, _ *string) ([]*models.Provider, error) {
			return []*models.Provider{
				{ID: "kubernetes-default", Platform: "kubernetes"},
			}, nil
		},
	}

	adapter := &testDeploymentAdapter{
		discoverFn: func(_ context.Context, _ string) ([]*models.Deployment, error) {
			discoverCalled = true
			return []*models.Deployment{}, nil
		},
	}

	svc := &registryServiceImpl{
		storeDB: mockDB,
		deploymentAdapters: map[string]registrytypes.DeploymentPlatformAdapter{
			"kubernetes": adapter,
		},
	}

	filter := &models.DeploymentFilter{Origin: &originManaged}
	got, err := svc.ListDeployments(context.Background(), filter)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.False(t, discoverCalled)
	assert.Equal(t, "dep-managed-only", got[0].ID)
}

func TestGetDeploymentByID_FallsBackToDiscoveredDeployments(t *testing.T) {
	discoveredID := deploymentsvc.DiscoveredDeploymentID("kubernetes-default", "mcp", "io.test/external", "unknown")
	mockDB := &deploymentMockDB{
		getDeploymentByIDFn: func(_ context.Context, _ string) (*models.Deployment, error) {
			return nil, database.ErrNotFound
		},
		getDeploymentsFn: func(_ context.Context, filter *models.DeploymentFilter) ([]*models.Deployment, error) {
			require.NotNil(t, filter)
			require.NotNil(t, filter.Origin)
			require.Equal(t, "discovered", *filter.Origin)
			return []*models.Deployment{}, nil
		},
		listProvidersFn: func(_ context.Context, _ *string) ([]*models.Provider, error) {
			return []*models.Provider{
				{ID: "kubernetes-default", Platform: "kubernetes"},
			}, nil
		},
	}
	adapter := &testDeploymentAdapter{
		discoverFn: func(_ context.Context, providerID string) ([]*models.Deployment, error) {
			return []*models.Deployment{
				{
					ServerName:   "io.test/external",
					Version:      "unknown",
					ResourceType: "mcp",
					Status:       "deployed",
					Origin:       "discovered",
					ProviderID:   providerID,
				},
			}, nil
		},
	}

	svc := &registryServiceImpl{
		storeDB: mockDB,
		deploymentAdapters: map[string]registrytypes.DeploymentPlatformAdapter{
			"kubernetes": adapter,
		},
	}

	got, err := svc.GetDeployment(context.Background(), discoveredID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, discoveredID, got.ID)
	assert.Equal(t, "discovered", got.Origin)
	assert.Equal(t, "kubernetes-default", got.ProviderID)
}

// promptMockDB is a minimal mock for database.Store that only implements
// GetPrompt and GetPromptVersion for testing ResolveAgentManifestPrompts.
type promptMockDB struct {
	database.Store
	getPromptByNameFn           func(ctx context.Context, name string) (*models.PromptResponse, error)
	getPromptByNameAndVersionFn func(ctx context.Context, name, version string) (*models.PromptResponse, error)
}

func (m *promptMockDB) Prompts() database.PromptStore {
	return m
}

func (m *promptMockDB) CreatePrompt(context.Context, *models.PromptJSON, *models.PromptRegistryExtensions) (*models.PromptResponse, error) {
	return nil, database.ErrInvalidInput
}

func (m *promptMockDB) ListPrompts(context.Context, *database.PromptFilter, string, int) ([]*models.PromptResponse, string, error) {
	return nil, "", nil
}

func (m *promptMockDB) GetPrompt(ctx context.Context, name string) (*models.PromptResponse, error) {
	if m.getPromptByNameFn != nil {
		return m.getPromptByNameFn(ctx, name)
	}
	return nil, database.ErrNotFound
}

func (m *promptMockDB) GetPromptVersion(ctx context.Context, name, version string) (*models.PromptResponse, error) {
	return m.getPromptByNameAndVersionFn(ctx, name, version)
}

func (m *promptMockDB) GetPromptVersions(context.Context, string) ([]*models.PromptResponse, error) {
	return nil, database.ErrNotFound
}

func (m *promptMockDB) GetLatestPrompt(context.Context, string) (*models.PromptResponse, error) {
	return nil, database.ErrNotFound
}

func (m *promptMockDB) CountPromptVersions(context.Context, string) (int, error) {
	return 0, nil
}

func (m *promptMockDB) CheckPromptVersionExists(context.Context, string, string) (bool, error) {
	return false, nil
}

func (m *promptMockDB) UnmarkPromptAsLatest(context.Context, string) error {
	return nil
}

func (m *promptMockDB) DeletePrompt(context.Context, string, string) error {
	return nil
}

func TestResolveAgentManifestPrompts(t *testing.T) {
	tests := []struct {
		name       string
		manifest   *models.AgentManifest
		dbFn       func(ctx context.Context, name, version string) (*models.PromptResponse, error)
		dbByNameFn func(ctx context.Context, name string) (*models.PromptResponse, error)
		want       []api.ResolvedPrompt
		wantErr    string
	}{
		{
			name:     "nil manifest returns nil",
			manifest: nil,
			want:     nil,
		},
		{
			name:     "empty prompts returns nil",
			manifest: &models.AgentManifest{Prompts: []models.PromptRef{}},
			want:     nil,
		},
		{
			name: "single prompt with explicit version",
			manifest: &models.AgentManifest{
				Prompts: []models.PromptRef{
					{Name: "my-system-prompt", RegistryPromptName: "system-prompt", RegistryPromptVersion: "2.0.0"},
				},
			},
			dbFn: func(_ context.Context, name, version string) (*models.PromptResponse, error) {
				if name == "system-prompt" && version == "2.0.0" {
					return &models.PromptResponse{
						Prompt: models.PromptJSON{Name: "system-prompt", Version: "2.0.0", Content: "You are a coding assistant."},
					}, nil
				}
				return nil, database.ErrNotFound
			},
			want: []api.ResolvedPrompt{
				{Name: "my-system-prompt", Content: "You are a coding assistant."},
			},
		},
		{
			name: "version defaults to latest when empty",
			manifest: &models.AgentManifest{
				Prompts: []models.PromptRef{
					{Name: "safety", RegistryPromptName: "safety-prompt"},
				},
			},
			dbByNameFn: func(_ context.Context, name string) (*models.PromptResponse, error) {
				if name == "safety-prompt" {
					return &models.PromptResponse{
						Prompt: models.PromptJSON{Name: "safety-prompt", Version: "1.2.0", Content: "Be safe."},
					}, nil
				}
				return nil, fmt.Errorf("unexpected lookup: %s", name)
			},
			want: []api.ResolvedPrompt{
				{Name: "safety", Content: "Be safe."},
			},
		},
		{
			name: "display name falls back to registry prompt name",
			manifest: &models.AgentManifest{
				Prompts: []models.PromptRef{
					{RegistryPromptName: "fallback-prompt", RegistryPromptVersion: "1.0.0"},
				},
			},
			dbFn: func(_ context.Context, name, version string) (*models.PromptResponse, error) {
				return &models.PromptResponse{
					Prompt: models.PromptJSON{Name: name, Version: version, Content: "Fallback content."},
				}, nil
			},
			want: []api.ResolvedPrompt{
				{Name: "fallback-prompt", Content: "Fallback content."},
			},
		},
		{
			name: "multiple prompts resolved in order",
			manifest: &models.AgentManifest{
				Prompts: []models.PromptRef{
					{Name: "first", RegistryPromptName: "prompt-a", RegistryPromptVersion: "1.0.0"},
					{Name: "second", RegistryPromptName: "prompt-b", RegistryPromptVersion: "2.0.0"},
				},
			},
			dbFn: func(_ context.Context, name, version string) (*models.PromptResponse, error) {
				switch name {
				case "prompt-a":
					return &models.PromptResponse{
						Prompt: models.PromptJSON{Name: name, Version: version, Content: "Content A"},
					}, nil
				case "prompt-b":
					return &models.PromptResponse{
						Prompt: models.PromptJSON{Name: name, Version: version, Content: "Content B"},
					}, nil
				}
				return nil, database.ErrNotFound
			},
			want: []api.ResolvedPrompt{
				{Name: "first", Content: "Content A"},
				{Name: "second", Content: "Content B"},
			},
		},
		{
			name: "empty prompt name returns error",
			manifest: &models.AgentManifest{
				Prompts: []models.PromptRef{
					{Name: "bad-ref", RegistryPromptName: ""},
				},
			},
			wantErr: "prompt name is required",
		},
		{
			name: "whitespace-only prompt name returns error",
			manifest: &models.AgentManifest{
				Prompts: []models.PromptRef{
					{Name: "blank", RegistryPromptName: "   "},
				},
			},
			wantErr: "prompt name is required",
		},
		{
			name: "db lookup failure propagates error",
			manifest: &models.AgentManifest{
				Prompts: []models.PromptRef{
					{Name: "missing", RegistryPromptName: "nonexistent", RegistryPromptVersion: "1.0.0"},
				},
			},
			dbFn: func(_ context.Context, _, _ string) (*models.PromptResponse, error) {
				return nil, database.ErrNotFound
			},
			wantErr: `resolve prompt "nonexistent" version "1.0.0"`,
		},
		{
			name: "error on second prompt does not return partial results",
			manifest: &models.AgentManifest{
				Prompts: []models.PromptRef{
					{Name: "ok", RegistryPromptName: "good-prompt", RegistryPromptVersion: "1.0.0"},
					{Name: "bad", RegistryPromptName: "bad-prompt", RegistryPromptVersion: "1.0.0"},
				},
			},
			dbFn: func(_ context.Context, name, _ string) (*models.PromptResponse, error) {
				if name == "good-prompt" {
					return &models.PromptResponse{
						Prompt: models.PromptJSON{Name: name, Version: "1.0.0", Content: "Good"},
					}, nil
				}
				return nil, fmt.Errorf("db connection lost")
			},
			wantErr: `resolve prompt "bad-prompt" version "1.0.0"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockDB := &promptMockDB{
				getPromptByNameFn:           tt.dbByNameFn,
				getPromptByNameAndVersionFn: tt.dbFn,
			}
			svc := &registryServiceImpl{storeDB: mockDB}

			got, err := svc.ResolveAgentManifestPrompts(context.Background(), tt.manifest)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
