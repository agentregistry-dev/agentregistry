package registries_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1/registries"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidateNPM_RegistryBaseURLOverride locks in @josh-pritchard's
// requested behavior: a non-canonical RegistryBaseURL must be honored
// as a private-mirror override, not rejected. Used to bail with
// "registry type and base URL do not match"; now the validator's HTTP
// probe is routed to whatever URL the package supplied.
func TestValidateNPM_RegistryBaseURLOverride(t *testing.T) {
	const serverName = "io.example/private-server"
	var probedPath string
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"mcpName":"` + serverName + `"}`))
	}))
	defer mirror.Close()

	err := registries.ValidateNPM(context.Background(),
		v1alpha1.RegistryPackage{
			RegistryType:    v1alpha1.RegistryTypeNPM,
			Identifier:      "my-pkg",
			Version:         "1.0.0",
			RegistryBaseURL: mirror.URL,
		},
		serverName,
	)
	require.NoError(t, err, "non-canonical RegistryBaseURL must be honored as override")
	require.Equal(t, "/my-pkg/1.0.0", probedPath, "validator must route HTTP probe to the override URL")
}

func TestValidateNPM_RealPackages(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		packageName  string
		version      string
		serverName   string
		expectError  bool
		errorMessage string
	}{
		{
			name:         "empty package identifier should fail",
			packageName:  "",
			version:      "1.0.0",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "package identifier is required for NPM packages",
		},
		{
			name:         "empty package version should fail",
			packageName:  "test-package",
			version:      "",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "package version is required for NPM packages",
		},
		{
			name:         "both empty identifier and version should fail with identifier error first",
			packageName:  "",
			version:      "",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "package identifier is required for NPM packages",
		},
		{
			name:         "non-existent package should fail",
			packageName:  generateRandomPackageName(),
			version:      "1.0.0",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "not found",
		},
		{
			name:         "real package without mcpName should fail",
			packageName:  "express", // Popular package without mcpName field
			version:      "4.18.2",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "missing required 'mcpName' field",
		},
		{
			name:         "real package without mcpName should fail",
			packageName:  "lodash", // Another popular package
			version:      "4.17.21",
			serverName:   "com.example/completely-different-name",
			expectError:  true,
			errorMessage: "missing required 'mcpName' field",
		},
		{
			name:         "real package without mcpName should fail",
			packageName:  "airtable-mcp-server",
			version:      "1.5.0",
			serverName:   "io.github.domdomegg/airtable-mcp-server",
			expectError:  true,
			errorMessage: "missing required 'mcpName' field",
		},
		{
			name:         "real package with incorrect mcpName should fail",
			packageName:  "airtable-mcp-server",
			version:      "1.7.2",
			serverName:   "io.github.not-domdomegg/airtable-mcp-server",
			expectError:  true,
			errorMessage: "Expected mcpName 'io.github.not-domdomegg/airtable-mcp-server', got 'io.github.domdomegg/airtable-mcp-server'",
		},
		{
			name:        "real package with correct mcpName should pass",
			packageName: "airtable-mcp-server",
			version:     "1.7.2",
			serverName:  "io.github.domdomegg/airtable-mcp-server",
			expectError: false,
		},
		{
			name:         "scoped package that doesn't exist should fail",
			packageName:  "@nonexistent-scope/nonexistent-package",
			version:      "1.0.0",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "not found",
		},
		{
			name:         "scoped package without mcpName should fail",
			packageName:  "@types/node",
			version:      "20.0.0",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "missing required 'mcpName' field",
		},
		{
			name:        "scoped package with mcpName should pass",
			packageName: "@hellocoop/admin-mcp",
			version:     "1.5.7",
			serverName:  "io.github.hellocoop/admin-mcp",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := v1alpha1.RegistryPackage{
				RegistryType: v1alpha1.RegistryTypeNPM,
				Identifier:   tt.packageName,
				Version:      tt.version,
			}

			err := registries.ValidateNPM(ctx, pkg, tt.serverName)

			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMessage)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
