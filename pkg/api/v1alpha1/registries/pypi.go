package registries

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

var (
	ErrMissingIdentifierForPyPI = errors.New("package identifier is required for PyPI packages")
	ErrMissingVersionForPyPi    = errors.New("package version is required for PyPI packages")
)

// PyPIPackageResponse represents the structure returned by the PyPI JSON API
type PyPIPackageResponse struct {
	Info struct {
		Description string `json:"description"`
	} `json:"info"`
}

// ValidatePyPI validates that a PyPI package contains the correct MCP server name
func ValidatePyPI(ctx context.Context, pkg v1alpha1.RegistryPackage, serverName string) error {
	// RegistryBaseURL is honored as an override — empty falls back to
	// the canonical default, non-empty drives the probe directly so
	// private mirrors (devpi etc.) work without OSS patching.
	if pkg.RegistryBaseURL == "" {
		pkg.RegistryBaseURL = DefaultURLPyPI
	}

	if pkg.Identifier == "" {
		return ErrMissingIdentifierForPyPI
	}

	if pkg.Version == "" {
		return ErrMissingVersionForPyPi
	}

	// Validate that MCPB-specific fields are not present
	if pkg.FileSHA256 != "" {
		return fmt.Errorf("PyPI packages must not have 'fileSha256' field - this is only for MCPB packages")
	}

	client := &http.Client{Timeout: 10 * time.Second}

	url := fmt.Sprintf("%s/pypi/%s/%s/json", pkg.RegistryBaseURL, pkg.Identifier, pkg.Version)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "agent-registry-Validator/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch package metadata from PyPI: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("PyPI package '%s' not found (status: %d)", pkg.Identifier, resp.StatusCode)
	}

	var pypiResp PyPIPackageResponse
	if err := json.NewDecoder(resp.Body).Decode(&pypiResp); err != nil {
		return fmt.Errorf("failed to parse PyPI package metadata: %w", err)
	}

	// Check description (README) content
	description := pypiResp.Info.Description

	// Check for mcp-name: format (more specific)
	mcpNamePattern := "mcp-name: " + serverName
	if strings.Contains(description, mcpNamePattern) {
		return nil // Found as mcp-name: format
	}

	return fmt.Errorf("PyPI package '%s' ownership validation failed. The server name '%s' must appear as 'mcp-name: %s' in the package README", pkg.Identifier, serverName, serverName)
}
