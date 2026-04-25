package registries_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1/registries"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidatePyPI_RegistryBaseURLOverride mirrors the NPM override
// test for PyPI. A package supplying its own RegistryBaseURL routes
// the JSON-API probe to that mirror without the validator rejecting
// it for being non-canonical.
func TestValidatePyPI_RegistryBaseURLOverride(t *testing.T) {
	const serverName = "io.example/private-server"
	var probedPath string
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"info":{"description":"# README\n\nmcp-name: ` + serverName + `\n"}}`))
	}))
	defer mirror.Close()

	err := registries.ValidatePyPI(context.Background(),
		v1alpha1.RegistryPackage{
			RegistryType:    v1alpha1.RegistryTypePyPI,
			Identifier:      "my-pkg",
			Version:         "1.0.0",
			RegistryBaseURL: mirror.URL,
		},
		serverName,
	)
	require.NoError(t, err, "non-canonical RegistryBaseURL must be honored as override")
	require.Equal(t, "/pypi/my-pkg/1.0.0/json", probedPath, "validator must route HTTP probe to the override URL")
}

func TestValidatePyPI_RealPackages(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name          string
		packageName   string
		version       string
		serverName    string
		expectError   bool
		errorMessage  string
		networkBound  bool // true if test depends on PyPI being reachable
		allowNetError bool // true if network errors are acceptable (e.g. non-existent package)
	}{
		{
			name:         "empty package identifier should fail",
			packageName:  "",
			version:      "1.0.0",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "package identifier is required for PyPI packages",
		},
		{
			name:         "empty package version should fail",
			packageName:  "mcp-server-example",
			version:      "",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "package version is required for PyPI packages",
		},
		{
			name:          "non-existent package should fail",
			packageName:   generateRandomPackageName(),
			version:       "1.0.0",
			serverName:    "com.example/test",
			expectError:   true,
			errorMessage:  "not found",
			networkBound:  true,
			allowNetError: true,
		},
		{
			name:         "real package without MCP server name should fail",
			packageName:  "requests", // Popular package without MCP server name in keywords/description/URLs
			version:      "2.31.0",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "ownership validation failed",
			networkBound: true,
		},
		{
			name:         "real package with different server name should fail",
			packageName:  "numpy", // Another popular package
			version:      "1.25.2",
			serverName:   "com.example/completely-different-name",
			expectError:  true,
			errorMessage: "ownership validation failed", // Will fail because numpy doesn't have this server name
			networkBound: true,
		},
		{
			name:         "real package with server name in README should pass",
			packageName:  "time-mcp-pypi",
			version:      "1.0.6",
			serverName:   "io.github.domdomegg/time-mcp-pypi",
			expectError:  false,
			networkBound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := v1alpha1.RegistryPackage{
				RegistryType: v1alpha1.RegistryTypePyPI,
				Identifier:   tt.packageName,
				Version:      tt.version,
			}

			err := registries.ValidatePyPI(ctx, pkg, tt.serverName)

			if tt.expectError {
				require.Error(t, err)
				errMsg := err.Error()
				// For network-bound tests, a timeout or connection error is
				// an acceptable alternative to the expected message since the
				// test still proves the package cannot be validated.
				if tt.allowNetError && isNetworkError(errMsg) {
					return
				}
				if tt.networkBound && isNetworkError(errMsg) {
					t.Skipf("skipping due to transient network error: %v", err)
				}
				assert.Contains(t, errMsg, tt.errorMessage)
			} else {
				if err != nil && tt.networkBound && isNetworkError(err.Error()) {
					t.Skipf("skipping due to transient network error: %v", err)
				}
				require.NoError(t, err)
			}
		})
	}
}

// isNetworkError returns true if the error message indicates a transient
// network issue (timeout, DNS failure, connection refused, etc.).
func isNetworkError(msg string) bool {
	patterns := []string{
		"context deadline exceeded",
		"connection refused",
		"no such host",
		"i/o timeout",
		"TLS handshake timeout",
	}
	for _, p := range patterns {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}
