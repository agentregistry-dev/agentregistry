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

// TestValidateNuGet_RegistryBaseURLOverride mirrors the NPM/PyPI
// override tests. A package supplying its own RegistryBaseURL routes
// the README probe to that mirror — used to be rejected with
// "registry type and base URL do not match", now accepted as the
// private-feed override that operators want.
func TestValidateNuGet_RegistryBaseURLOverride(t *testing.T) {
	const serverName = "io.example/private-server"
	var probedPath string
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probedPath = r.URL.Path
		_, _ = w.Write([]byte("# README\n\nmcp-name: " + serverName + "\n"))
	}))
	defer mirror.Close()

	err := registries.ValidateNuGet(context.Background(),
		v1alpha1.RegistryPackage{
			RegistryType:    v1alpha1.RegistryTypeNuGet,
			Identifier:      "my-pkg",
			Version:         "1.0.0",
			RegistryBaseURL: mirror.URL,
		},
		serverName,
	)
	require.NoError(t, err, "non-canonical RegistryBaseURL must be honored as override")
	require.Equal(t, "/v3-flatcontainer/my-pkg/1.0.0/readme", probedPath, "validator must route HTTP probe to the override URL")
}

func TestValidateNuGet_RealPackages(t *testing.T) {
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
			errorMessage: "package identifier is required for NuGet packages",
		},
		{
			name:         "empty package version should fail",
			packageName:  "test-package",
			version:      "",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "package version is required for NuGet packages",
		},
		{
			name:         "both empty identifier and version should fail with identifier error first",
			packageName:  "",
			version:      "",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "package identifier is required for NuGet packages",
		},
		{
			name:         "non-existent package should fail",
			packageName:  generateRandomNuGetPackageName(),
			version:      "1.0.0",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "ownership validation failed",
		},
		{
			name:         "real package without version should fail",
			packageName:  "Newtonsoft.Json",
			version:      "", // No version provided
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "package version is required for NuGet packages",
		},
		{
			name:         "real package with non-existent version should fail",
			packageName:  "Newtonsoft.Json",
			version:      "999.999.999", // Version that doesn't exist
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "ownership validation failed",
		},
		{
			name:         "real package without server name in README should fail",
			packageName:  "Newtonsoft.Json",
			version:      "13.0.3", // Popular version
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "ownership validation failed",
		},
		{
			name:         "real package without server name in README should fail",
			packageName:  "TimeMcpServer",
			version:      "1.0.0",
			serverName:   "io.github.domdomegg/time-mcp-server",
			expectError:  true,
			errorMessage: "ownership validation failed",
		},
		{
			name:        "real package with server name in README should pass",
			packageName: "TimeMcpServer",
			version:     "1.0.2",
			serverName:  "io.github.domdomegg/time-mcp-server",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := v1alpha1.RegistryPackage{
				RegistryType: v1alpha1.RegistryTypeNuGet,
				Identifier:   tt.packageName,
				Version:      tt.version,
			}

			err := registries.ValidateNuGet(ctx, pkg, tt.serverName)

			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMessage)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
