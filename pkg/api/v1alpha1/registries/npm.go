package registries

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

var (
	ErrMissingIdentifierForNPM = errors.New("package identifier is required for NPM packages")
	ErrMissingVersionForNPM    = errors.New("package version is required for NPM packages")
)

// NPMPackageResponse represents the structure returned by the NPM registry API
type NPMPackageResponse struct {
	MCPName string `json:"mcpName"`
}

// ValidateNPM validates that an NPM package contains the correct MCP server name
func ValidateNPM(ctx context.Context, pkg v1alpha1.RegistryPackage, serverName string) error {
	// RegistryBaseURL is honored as an override — empty falls back to
	// the canonical default, non-empty drives the probe directly so
	// private mirrors (Verdaccio etc.) work without OSS patching.
	if pkg.RegistryBaseURL == "" {
		pkg.RegistryBaseURL = DefaultURLNPM
	}

	if pkg.Identifier == "" {
		return ErrMissingIdentifierForNPM
	}

	// we need version to look up the package metadata
	// not providing version will return all the versions
	// and we won't be able to validate the mcpName field
	// against the server name
	if pkg.Version == "" {
		return ErrMissingVersionForNPM
	}

	// Validate that MCPB-specific fields are not present
	if pkg.FileSHA256 != "" {
		return fmt.Errorf("NPM packages must not have 'fileSha256' field")
	}

	client := &http.Client{Timeout: 10 * time.Second}

	requestURL := pkg.RegistryBaseURL + "/" + url.PathEscape(pkg.Identifier) + "/" + url.PathEscape(pkg.Version)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "agent-registry-Validator/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch package metadata from NPM: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("NPM package '%s' not found (status: %d)", pkg.Identifier, resp.StatusCode)
	}

	var npmResp NPMPackageResponse
	if err := json.NewDecoder(resp.Body).Decode(&npmResp); err != nil {
		return fmt.Errorf("failed to parse NPM package metadata: %w", err)
	}

	if npmResp.MCPName == "" {
		return fmt.Errorf("NPM package '%s' is missing required 'mcpName' field. Add this to your package.json: \"mcpName\": \"%s\"", pkg.Identifier, serverName)
	}

	if npmResp.MCPName != serverName {
		return fmt.Errorf("NPM package ownership validation failed. Expected mcpName '%s', got '%s'", serverName, npmResp.MCPName)
	}

	return nil
}
