package types

import (
	"context"
	"errors"

	"github.com/spf13/cobra"
)

// ErrCLINoStoredToken is returned when no stored authentication token is found.
// This is expected for CLI commands that do not require authentication
// (e.g. `arctl init`).
var ErrCLINoStoredToken = errors.New("no stored authentication token")

// ErrNoOIDCDefined is returned when OIDC is not defined.
// This is expected for CLI commands that do not require authentication
// (e.g. `arctl init`) when the user/extension has not configured OIDC.
var ErrNoOIDCDefined = errors.New("OIDC is not defined")

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
