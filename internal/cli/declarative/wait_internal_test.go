package declarative

import (
	"context"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/agentregistry-dev/agentregistry/pkg/cli/runtime"
)

// When the registry runtime is missing, runDeclarativeWait returns the typed sentinel so
// callers can errors.Is against it.
func TestRunDeclarativeWait_APIClientNotInitializedIsTyped(t *testing.T) {
	cmd := NewWaitCmd(cliruntime.Deps{})
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"deployment", "summarizer"})

	err := runDeclarativeWait(cmd, cliruntime.Deps{}, []string{"deployment", "summarizer"})
	require.Error(t, err)
	assert.ErrorIs(t, err, errRegistryRuntimeNotConfigured)
}

// Compile-time check that NewWaitCmd still returns a cobra command with RunE set.
var _ func(*cobra.Command, []string) error = NewWaitCmd(internalDeclarativeTestDeps(nil)).RunE
