package deployment

import (
	"errors"
	"fmt"
	"strings"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

// UnsupportedDeploymentRuntimeError reports that no deployment adapter
// exists for a runtime type. Coordinator returns this when the runtime's
// Spec.Type string has no registered adapter so callers (MCP tool
// surface, HTTP handler) can distinguish "no adapter" from transient
// plumbing failures.
type UnsupportedDeploymentRuntimeError struct {
	Type string
}

func (e *UnsupportedDeploymentRuntimeError) Error() string {
	runtimeType := strings.TrimSpace(e.Type)
	if runtimeType == "" {
		runtimeType = "unknown"
	}
	return fmt.Sprintf("unsupported deployment runtime: %s", runtimeType)
}

func (e *UnsupportedDeploymentRuntimeError) Unwrap() error {
	return database.ErrInvalidInput
}

// IsUnsupportedDeploymentRuntimeError reports whether err wraps an
// UnsupportedDeploymentRuntimeError.
func IsUnsupportedDeploymentRuntimeError(err error) bool {
	var target *UnsupportedDeploymentRuntimeError
	return errors.As(err, &target)
}
