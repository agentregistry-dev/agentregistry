// Package inspector launches MCP Inspector as a subprocess against a given
// MCP server URL. The implementation is a thin wrapper around
// `npx -y @modelcontextprotocol/inspector --server-url <url>` — it has no
// independent protocol handling. The caller owns the returned process's
// lifecycle and must call Process.Kill() on shutdown.
package inspector

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// commandFactory matches the signature of exec.CommandContext so tests can
// inject a fake without invoking npx.
type commandFactory func(ctx context.Context, name string, args ...string) *exec.Cmd

// starter matches the signature of (*exec.Cmd).Start so tests can avoid
// spawning processes during unit tests.
type starter func(cmd *exec.Cmd) error

// Launch starts MCP Inspector as a subprocess pointed at serverURL.
// The subprocess's stdout/stderr are wired to the current process's streams.
// Returns the *exec.Cmd so the caller can call Process.Kill() on shutdown.
// Returns an error only if the subprocess fails to start (typically because
// npx is not on PATH).
func Launch(ctx context.Context, serverURL string) (*exec.Cmd, error) {
	return launchWith(ctx, serverURL, exec.CommandContext, func(c *exec.Cmd) error { return c.Start() })
}

func launchWith(ctx context.Context, serverURL string, makeCmd commandFactory, start starter) (*exec.Cmd, error) {
	cmd := makeCmd(ctx, "npx", "-y", "@modelcontextprotocol/inspector", "--server-url", serverURL)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := start(cmd); err != nil {
		return nil, fmt.Errorf("starting MCP Inspector subprocess: %w", err)
	}
	return cmd, nil
}
