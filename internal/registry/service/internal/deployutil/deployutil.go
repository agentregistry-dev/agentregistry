package deployutil

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

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

func IsUnsupportedDeploymentPlatformError(err error) bool {
	var target *UnsupportedDeploymentPlatformError
	return errors.As(err, &target)
}

type PlatformStaleCleaner interface {
	CleanupStale(ctx context.Context, deployment *models.Deployment) error
}
