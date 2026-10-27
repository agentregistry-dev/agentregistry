package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"time"

	"github.com/agentregistry-dev/agentregistry/pkg/registry"
)

func main() {
	// The runtime image has no shell, so container healthchecks call the binary.
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		if err := healthcheck(os.Getenv("AGENT_REGISTRY_SERVER_ADDRESS")); err != nil {
			fmt.Fprintln(os.Stderr, err)
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

// healthcheck dials the server's listen address over loopback.
func healthcheck(addr string) error {
	if addr == "" {
		addr = ":8080"
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid server address %q: %w", addr, err)
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 2*time.Second)
	if err != nil {
		return err
	}
	return conn.Close()
}
