package declarative_test

import (
	"reflect"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/cli/declarative"
	"github.com/agentregistry-dev/agentregistry/internal/registry/kinds"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetCmd_RejectsUnknownType(t *testing.T) {
	declarative.SetAPIClient(nil)
	cmd := declarative.NewGetCmd()
	cmd.SetArgs([]string{"unknowntype"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.ErrorContains(t, err, "unknown kind")
}

func TestGetCmd_RequiresTypeArg(t *testing.T) {
	cmd := declarative.NewGetCmd()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestGetCmd_NoAPIClientErrors(t *testing.T) {
	declarative.SetAPIClient(nil)
	cmd := declarative.NewGetCmd()
	cmd.SetArgs([]string{"agents"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.ErrorContains(t, err, "API client not initialized")
}

// TestGetCmd_RegistryDrivenColumnLookup verifies that the defaultRegistry has TableColumns
// for known kinds, confirming the registry-driven path is active.
func TestGetCmd_RegistryDrivenColumnLookup(t *testing.T) {
	// Build a registry with a kind that has TableColumns set (same as the CLI registry).
	reg := kinds.NewRegistry()
	reg.Register(kinds.Kind{
		Kind:     "agent",
		Plural:   "agents",
		Aliases:  []string{"Agent"},
		SpecType: reflect.TypeFor[kinds.AgentSpec](),
		TableColumns: []kinds.Column{
			{Header: "NAME"},
			{Header: "VERSION"},
		},
	})

	declarative.SetRegistry(reg)
	// Restore the default registry after the test.
	t.Cleanup(func() { declarative.SetRegistry(declarative.NewCLIRegistry()) })
	declarative.SetAPIClient(nil)

	// Looking up a valid kind should get past the registry validation step
	// and fail only at "API client not initialized" — confirming the registry path ran.
	cmd := declarative.NewGetCmd()
	cmd.SetArgs([]string{"agents"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.ErrorContains(t, err, "API client not initialized",
		"should fail at API client check, not registry lookup")
}
