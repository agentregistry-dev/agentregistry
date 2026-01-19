package main

import (
	"context"
	"log"

	"github.com/agentregistry-dev/agentregistry/pkg/registry"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

func main() {
	ctx := context.Background()
	if err := registry.App(ctx, types.AppOptions{
		// We do not have an approval process, therefore we auto-approve all artifacts.
		AutoApproveArtifacts: true,
	}); err != nil {
		log.Fatalf("Failed to start registry: %v", err)
	}
}
