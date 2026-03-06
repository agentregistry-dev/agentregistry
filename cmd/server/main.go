package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/agentregistry-dev/agentregistry/pkg/registry"
)

func main() {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	logger := slog.New(handler)
	slog.SetDefault(logger)

	ctx := context.Background()
	if err := registry.App(ctx); err != nil {
		slog.Error("failed to start registry", "error", err)
		os.Exit(1)
	}
}
