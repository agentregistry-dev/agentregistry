// Package registries ports the per-registry validators from
// internal/registry/validators/registries onto the v1alpha1.RegistryPackage
// view. Each validator (OCI, NPM, PyPI, NuGet, MCPB) is a standalone
// function that hits the corresponding external registry to confirm
// the package exists + (where supported) carries an ownership
// annotation matching the resource's metadata.name.
//
// git mv preserves authorship of each file through the move; the
// only byte-level change on each moved file is the signature swap
// from modelcontextprotocol/registry model.Package to
// pkg/api/v1alpha1.RegistryPackage.
package registries

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

var (
	ErrMissingIdentifierForOCI = errors.New("package identifier is required for OCI packages")
	ErrUnsupportedRegistry     = errors.New("unsupported OCI registry")
)

// ErrRateLimited is returned when a registry rate limits our requests
var ErrRateLimited = errors.New("rate limited by registry")

// allowedOCIRegistries defines the list of supported OCI registries.
// This can be expanded in the future to support additional public registries.
var allowedOCIRegistries = map[string]bool{
	// Docker Hub (and its various endpoints)
	"docker.io":            true,
	"registry-1.docker.io": true, // Docker Hub API endpoint
	"index.docker.io":      true, // Docker Hub index
	// GitHub Container Registry
	"ghcr.io": true,
	// Google Artifact Registry (*.pkg.dev pattern handled in isAllowedRegistry)
}

// ValidateOCI validates that an OCI image contains the correct MCP server name annotation.
// Supports canonical OCI references including:
//   - registry/namespace/image:tag
//   - registry/namespace/image@sha256:digest
//   - registry/namespace/image:tag@sha256:digest
//   - namespace/image:tag (defaults to docker.io)
//
// Supported registries:
//   - Docker Hub (docker.io)
//   - GitHub Container Registry (ghcr.io)
//   - Google Artifact Registry (*.pkg.dev)
func ValidateOCI(ctx context.Context, pkg v1alpha1.RegistryPackage, serverName string) error {
	if pkg.Identifier == "" {
		return ErrMissingIdentifierForOCI
	}

	// Validate that old format fields are not present
	if pkg.RegistryBaseURL != "" {
		return fmt.Errorf("OCI packages must not have 'registryBaseUrl' field - use canonical reference in 'identifier' instead (e.g., 'docker.io/owner/image:1.0.0')")
	}
	if pkg.Version != "" {
		return fmt.Errorf("OCI packages must not have 'version' field - include version in 'identifier' instead (e.g., 'docker.io/owner/image:1.0.0')")
	}
	if pkg.FileSHA256 != "" {
		return fmt.Errorf("OCI packages must not have 'fileSha256' field")
	}

	// Parse the OCI reference using go-containerregistry's name package
	// This handles all the complexity of reference parsing including defaults
	ref, err := name.ParseReference(pkg.Identifier)
	if err != nil {
		return fmt.Errorf("invalid OCI reference: %w", err)
	}

	// Private / dev registries (localhost, 127.0.0.1, [::1]) are the default
	// target of `arctl build --push` and the registry server itself cannot
	// reach them anonymously from outside the developer's machine. Skip
	// allowlist enforcement and ownership validation for these — the
	// allowlist + label check exist to gate the public catalogue, and
	// private workflows pre-date that contract.
	registry := ref.Context().RegistryStr()
	if isPrivateRegistry(registry) {
		slog.Info("skipping OCI validation for private registry", "identifier", pkg.Identifier, "registry", registry)
		return nil
	}

	// Validate that the registry is in the allowlist
	if !isAllowedRegistry(registry) {
		return fmt.Errorf("%w: %s", ErrUnsupportedRegistry, registry)
	}

	// Add explicit timeout to prevent hanging on slow registries
	// Use a new context with timeout to avoid modifying the caller's context
	timeoutCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Fetch the image using anonymous authentication (public images only)
	// The go-containerregistry library handles:
	// - OCI auth discovery via WWW-Authenticate headers
	// - Token negotiation for different registries
	// - Rate limiting and retries
	// - Multi-arch manifest resolution
	img, err := remote.Image(ref, remote.WithAuth(authn.Anonymous), remote.WithContext(timeoutCtx))
	if err != nil {
		// Check if this is a timeout error
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("OCI image validation timed out after 30 seconds for '%s'. The registry may be slow or unreachable", pkg.Identifier)
		}

		// Check for specific HTTP status codes
		var transportErr *transport.Error
		if errors.As(err, &transportErr) {
			switch transportErr.StatusCode {
			case http.StatusTooManyRequests:
				// Rate limited - skip validation to avoid blocking publishers
				// This is intentional: we prioritize UX over strict validation during high traffic
				slog.Info("skipping OCI validation due to rate limiting", "identifier", pkg.Identifier)
				return nil
			case http.StatusNotFound:
				return fmt.Errorf("OCI image '%s' does not exist in the registry", pkg.Identifier)
			case http.StatusUnauthorized, http.StatusForbidden:
				return fmt.Errorf("OCI image '%s' is private or requires authentication. Only public images are supported", pkg.Identifier)
			}
		}
		return fmt.Errorf("failed to fetch OCI image: %w", err)
	}

	// Get the image config which contains labels
	configFile, err := img.ConfigFile()
	if err != nil {
		return fmt.Errorf("failed to get image config: %w", err)
	}

	// Validate the MCP server name label
	if configFile.Config.Labels == nil {
		return fmt.Errorf("OCI image '%s' is missing required annotation. Add this to your Dockerfile: LABEL io.modelcontextprotocol.server.name=\"%s\"", pkg.Identifier, serverName)
	}

	mcpName, exists := configFile.Config.Labels["io.modelcontextprotocol.server.name"]
	if !exists {
		return fmt.Errorf("OCI image '%s' is missing required annotation. Add this to your Dockerfile: LABEL io.modelcontextprotocol.server.name=\"%s\"", pkg.Identifier, serverName)
	}

	if mcpName != serverName {
		return fmt.Errorf("OCI image ownership validation failed. Expected annotation 'io.modelcontextprotocol.server.name' = '%s', got '%s'", serverName, mcpName)
	}

	return nil
}

// isAllowedRegistry checks if the given registry is in the allowlist.
// It handles registry aliases and wildcard patterns (e.g., *.pkg.dev for Artifact Registry).
func isAllowedRegistry(registry string) bool {
	// Direct match
	if allowedOCIRegistries[registry] {
		return true
	}

	// Check for wildcard patterns
	// Google Artifact Registry: *.pkg.dev (e.g., us-docker.pkg.dev, europe-west1-docker.pkg.dev)
	if strings.HasSuffix(registry, ".pkg.dev") {
		return true
	}

	return false
}

// isPrivateRegistry matches registry hosts that are local to the
// developer's machine (the `arctl build --push` default target). We skip
// both allowlist enforcement and network-ownership validation for these —
// the registry is unreachable from outside, and the allowlist exists to
// gate the public catalogue which private images are not part of.
func isPrivateRegistry(registry string) bool {
	// strip :port for the hostname check
	host := registry
	if i := strings.LastIndex(host, ":"); i >= 0 && !strings.Contains(host[i+1:], ".") {
		host = host[:i]
	}
	host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[") // bracketed IPv6
	switch host {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}
