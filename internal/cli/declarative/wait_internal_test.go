package declarative

import (
	"context"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// When apiClient is nil, runDeclarativeWait returns the typed sentinel so
// callers can errors.Is against it.
func TestRunDeclarativeWait_APIClientNotInitializedIsTyped(t *testing.T) {
	prev := apiClient
	apiClient = nil
	t.Cleanup(func() { apiClient = prev })

	cmd := newWaitCmd()
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"deployment", "summarizer"})

	err := runDeclarativeWait(cmd, []string{"deployment", "summarizer"})
	require.Error(t, err)
	assert.ErrorIs(t, err, errAPIClientNotInitialized)
}

// Compile-time check that runDeclarativeWait still matches the cobra RunE signature.
var _ func(*cobra.Command, []string) error = runDeclarativeWait
