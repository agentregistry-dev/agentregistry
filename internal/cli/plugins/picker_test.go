// internal/cli/plugins/picker_test.go
package plugins

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newRegistryFor(plugins ...*Plugin) *Registry {
	r := NewRegistry()
	for _, p := range plugins {
		_ = r.Add(p, SourceInTree)
	}
	return r
}

func TestPick_FlagsResolveDirectly(t *testing.T) {
	r := newRegistryFor(&Plugin{Name: "adk-python", Type: "agent", Framework: "adk", Language: "python"})
	got, err := Pick(PickOpts{
		Registry:  r,
		Type:      "agent",
		Framework: "adk",
		Language:  "python",
	})
	require.NoError(t, err)
	assert.Equal(t, "adk-python", got.Name)
}

func TestPick_FlagsErrorOnUnknown(t *testing.T) {
	r := newRegistryFor(&Plugin{Name: "adk-python", Type: "agent", Framework: "adk", Language: "python"})
	_, err := Pick(PickOpts{
		Registry:  r,
		Type:      "agent",
		Framework: "unknown",
		Language:  "python",
	})
	require.Error(t, err)
}

func TestPick_NoFlagsNoTTY_ErrorsWithListing(t *testing.T) {
	r := newRegistryFor(
		&Plugin{Name: "adk-python", Type: "agent", Framework: "adk", Language: "python"},
	)
	_, err := Pick(PickOpts{Registry: r, Type: "agent", NonInteractive: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "adk")
}
