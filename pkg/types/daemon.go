package types

import (
	"context"
	"errors"

	"github.com/spf13/cobra"
)

// Daemon + CLI-side extension points. These types are referenced by the
// `arctl` CLI (internal/cli/daemon, pkg/cli, pkg/daemon/dockercompose) —
// kept in pkg/types so they cross the CLI / library boundary without a
// cyclic import.

// ErrCLINoStoredToken is returned when no stored authentication token is found.
// This is expected for CLI commands that do not require authentication
// (e.g. `arctl init`).
var ErrCLINoStoredToken = errors.New("no stored authentication token")

// ErrNoOIDCDefined is returned when OIDC is not defined.
// This is expected for CLI commands that do not require authentication
// (e.g. `arctl init`) when the user/extension has not configured OIDC.
var ErrNoOIDCDefined = errors.New("OIDC is not defined")

// DaemonManager defines the interface for managing the CLI's backend daemon.
// External libraries can implement this to use their own orchestration.
type DaemonManager interface {
	// IsRunning checks if the daemon is currently running.
	IsRunning() bool
	// Start starts the daemon and waits until it's ready to serve requests.
	Start() error
	// Stop stops the daemon but preserves data volumes.
	Stop() error
	// Purge stops the daemon and removes all data volumes.
	Purge() error
}

// CLITokenProvider provides tokens for CLI commands.
// External libraries can implement this to support fetching tokens from
// defined sources.
type CLITokenProvider interface {
	// Token returns a token for API calls.
	Token(ctx context.Context) (token string, err error)
}

// CLITokenProviderFactory is a function type that creates a CLI token
// provider. The factory optionally receives the root command so the
// implementation can read command-specific configuration (e.g. flags).
type CLITokenProviderFactory func(root *cobra.Command) (CLITokenProvider, error)
