package registry

import (
	"context"
	"log"

	internalregistry "github.com/agentregistry-dev/agentregistry/internal/registry"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

func App(ctx context.Context, opts ...types.AppOptions) error {
	var appOpts types.AppOptions
	if len(opts) > 0 {
		appOpts = opts[0]
	}

	if err := internalregistry.App(ctx, appOpts); err != nil {
		log.Fatalf("Failed to start registry: %v", err)
	}
	return nil
}
