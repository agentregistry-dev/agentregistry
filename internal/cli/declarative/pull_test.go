package declarative_test

import (
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/cli/declarative"
	"github.com/stretchr/testify/require"
)

func TestPull_RejectsUnknownType(t *testing.T) {
	cmd := declarative.NewPullCmd()
	cmd.SetArgs([]string{"unknown", "foo"})
	require.Error(t, cmd.Execute())
}
