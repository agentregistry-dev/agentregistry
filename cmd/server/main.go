package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"time"

	"github.com/agentregistry-dev/agentregistry/pkg/registry"
)

func main() {
	// The image has no shell, so the compose healthcheck dials the server with this binary.
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		if _, err := net.DialTimeout("tcp", os.Args[2], 2*time.Second); err != nil {
			slog.Error("healthcheck failed", "error", err)
			os.Exit(1)
		}
		return
	}

	ctx := context.Background()
	if err := registry.App(ctx); err != nil {
		slog.Error("failed to start registry", "error", err)
		os.Exit(1)
	}
}
