package declarative_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/internal/cli/declarative"
)

func TestPull_RejectsUnknownType(t *testing.T) {
	cmd := declarative.NewPullCmd(declarativeTestDeps(nil))
	cmd.SetArgs([]string{"unknown", "foo"})
	require.Error(t, cmd.Execute())
}
