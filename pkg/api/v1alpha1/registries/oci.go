// Package registries holds the per-registry validators that confirm a
// package exists in its upstream registry and carries an ownership
// annotation matching the resource's expected server name. Each
// validator (OCI, NPM, PyPI) is a standalone function consumed by
// Dispatcher via the v1alpha1.RegistryValidatorFunc type.
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

// ValidateOCI validates that an OCI image contains the correct MCP
// server name annotation.
//
// Supported reference forms (Identifier must include an explicit tag or digest):
//   - registry/namespace/image:tag
//   - registry/namespace/image@sha256:digest
//   - registry/namespace/image:tag@sha256:digest
//   - namespace/image:tag (defaults to docker.io)
//
// Supported registries:
//   - Docker Hub (docker.io)
//   - GitHub Container Registry (ghcr.io)
//   - Google Artifact Registry (*.pkg.dev)
func ValidateOCI(ctx context.Context, origin v1alpha1.MCPPackageOrigin, serverName string) error {
	if origin.OCI == nil {
		return fmt.Errorf("OCI validator called without origin.OCI set")
	}
	if origin.Identifier == "" {
		return ErrMissingIdentifierForOCI
	}
	if !ociIdentifierHasTagOrDigest(origin.Identifier) {
		return fmt.Errorf("OCI identifier %q must include an explicit tag (e.g. ':1.0.0') or digest (e.g. '@sha256:...') — bare references would silently resolve ':latest'", origin.Identifier)
	}

	ref, err := name.ParseReference(origin.Identifier)
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
		slog.Info("skipping OCI validation for private registry", "identifier", origin.Identifier, "registry", registry)
		return nil
	}

	if !isAllowedRegistry(registry) {
		return fmt.Errorf("%w: %s", ErrUnsupportedRegistry, registry)
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	img, err := remote.Image(ref, remote.WithAuth(authn.Anonymous), remote.WithContext(timeoutCtx))
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("OCI image validation timed out after 30 seconds for '%s'. The registry may be slow or unreachable", origin.Identifier)
		}

		var transportErr *transport.Error
		if errors.As(err, &transportErr) {
			switch transportErr.StatusCode {
			case http.StatusTooManyRequests:
				slog.Info("skipping OCI validation due to rate limiting", "identifier", origin.Identifier)
				return nil
			case http.StatusNotFound:
				return fmt.Errorf("OCI image '%s' does not exist in the registry", origin.Identifier)
			case http.StatusUnauthorized, http.StatusForbidden:
				return fmt.Errorf("OCI image '%s' is private or requires authentication. Only public images are supported", origin.Identifier)
			}
		}
		return fmt.Errorf("failed to fetch OCI image: %w", err)
	}

	configFile, err := img.ConfigFile()
	if err != nil {
		return fmt.Errorf("failed to get image config: %w", err)
	}

	if configFile.Config.Labels == nil {
		return fmt.Errorf("OCI image '%s' is missing required annotation. Add this to your Dockerfile: LABEL io.modelcontextprotocol.server.name=\"%s\"", origin.Identifier, serverName)
	}

	mcpName, exists := configFile.Config.Labels["io.modelcontextprotocol.server.name"]
	if !exists {
		return fmt.Errorf("OCI image '%s' is missing required annotation. Add this to your Dockerfile: LABEL io.modelcontextprotocol.server.name=\"%s\"", origin.Identifier, serverName)
	}

	if mcpName != serverName {
		return fmt.Errorf("OCI image ownership validation failed. Expected annotation 'io.modelcontextprotocol.server.name' = '%s', got '%s'", serverName, mcpName)
	}

	return nil
}

// ociIdentifierHasTagOrDigest reports whether ref has an explicit tag
// (`:<tag>`) or digest (`@sha256:...`) after the final path component.
// Registry ports (e.g. `localhost:5000/foo`) are not mistaken for tags
// because they appear before any `/`.
func ociIdentifierHasTagOrDigest(ref string) bool {
	if strings.Contains(ref, "@") {
		return true
	}
	tail := ref
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		tail = ref[i+1:]
	}
	return strings.Contains(tail, ":")
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
