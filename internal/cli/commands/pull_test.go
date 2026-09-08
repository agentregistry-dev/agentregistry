package commands_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/internal/cli/commands"
)

func TestPull_RejectsUnknownType(t *testing.T) {
	cmd := commands.NewPullCmd(declarativeTestDeps(nil))
	cmd.SetArgs([]string{"unknown", "foo"})
	require.Error(t, cmd.Execute())
}
