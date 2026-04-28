package deployment

import (
	"errors"
	"fmt"
	"strings"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

// UnsupportedDeploymentPlatformError reports that no deployment adapter
// exists for a provider platform. Coordinator returns this when
// the provider's Spec.Platform string has no registered adapter so
// callers (MCP tool surface, HTTP handler) can distinguish "no adapter"
// from transient plumbing failures.
type UnsupportedDeploymentPlatformError struct {
	Platform string
}

func (e *UnsupportedDeploymentPlatformError) Error() string {
	platform := strings.TrimSpace(e.Platform)
	if platform == "" {
		platform = "unknown"
	}
	return fmt.Sprintf("unsupported deployment platform: %s", platform)
}

func (e *UnsupportedDeploymentPlatformError) Unwrap() error {
	return database.ErrInvalidInput
}

// IsUnsupportedDeploymentPlatformError reports whether err wraps an
// UnsupportedDeploymentPlatformError.
func IsUnsupportedDeploymentPlatformError(err error) bool {
	var target *UnsupportedDeploymentPlatformError
	return errors.As(err, &target)
}
