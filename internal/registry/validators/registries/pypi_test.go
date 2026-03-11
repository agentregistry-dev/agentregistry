package registries_test

import (
	"context"
	"strings"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/registry/validators/registries"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
			pkg := model.Package{
				RegistryType: model.RegistryTypePyPI,
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
