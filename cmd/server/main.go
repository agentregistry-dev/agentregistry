package main

import (
	"context"
	"log"

	"github.com/agentregistry-dev/agentregistry/internal/registry"
)

func main() {
	ctx := context.Background()
	if err := registry.App(ctx); err != nil {
		log.Fatalf("Failed to start registry: %v", err)
	}
}
