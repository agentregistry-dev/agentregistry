package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/agentregistry-dev/agentregistry/pkg/registry"
)

func main() {
	ctx := context.Background()
	if err := registry.App(ctx); err != nil {
		slog.Error("failed to start registry", "error", err)
		os.Exit(1)
	}
}
