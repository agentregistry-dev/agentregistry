package api

import (
	"context"
)

// RuntimeTranslator is the interface for translating registry objects to runtime configuration objects.
type RuntimeTranslator interface {
	TranslateRuntimeConfig(
		ctx context.Context,
		desired *DesiredState,
	) (*AIRuntimeConfig, error)
}
