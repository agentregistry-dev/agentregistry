// Package inspector launches MCP Inspector as a subprocess. Thin wrapper
// around `npx -y @modelcontextprotocol/inspector`; no protocol handling.
// Callers own the returned *exec.Cmd and decide whether to Wait or Kill.
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
// stdout/stderr are wired to the current process. Errors only if Start
// fails (typically: npx not on PATH).
func Launch(ctx context.Context, serverURL string) (*exec.Cmd, error) {
	return launchWith(ctx, serverURL, exec.CommandContext, func(c *exec.Cmd) error { return c.Start() })
}

// LaunchStdio starts MCP Inspector as a subprocess that spawns binCmd +
// binArgs over stdio. dir is the working directory the binary runs in
// (pass projectDir for source-relative paths). extraEnv augments the
// inherited environment.
func LaunchStdio(ctx context.Context, dir string, extraEnv []string, binCmd string, binArgs ...string) (*exec.Cmd, error) {
	return launchStdioWith(ctx, dir, extraEnv, binCmd, binArgs, exec.CommandContext, func(c *exec.Cmd) error { return c.Start() })
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

func launchStdioWith(ctx context.Context, dir string, extraEnv []string, binCmd string, binArgs []string, makeCmd commandFactory, start starter) (*exec.Cmd, error) {
	// `--` keeps commander.js inside the inspector from consuming server-side
	// flags like `--transport stdio` that overlap with the inspector's own CLI.
	args := append([]string{"-y", "@modelcontextprotocol/inspector", "--", binCmd}, binArgs...)
	cmd := makeCmd(ctx, "npx", args...)
	cmd.Dir = dir
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := start(cmd); err != nil {
		return nil, fmt.Errorf("starting MCP Inspector (stdio) subprocess: %w", err)
	}
	return cmd, nil
}
