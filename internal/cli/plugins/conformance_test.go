package plugins

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Every embedded plugin must parse, type-check, and have at least a build
// command (or script). Catches malformed plugin.yaml at compile time.
func TestEmbeddedPlugins_ConformToContract(t *testing.T) {
	plugins, err := LoadEmbedded(t.TempDir())
	require.NoError(t, err)
	require.NotEmpty(t, plugins, "no embedded plugins found — the in-tree set is empty")

	for _, p := range plugins {
		t.Run(p.Name, func(t *testing.T) {
			require.NotEmpty(t, p.APIVersion, "%s: apiVersion required", p.Name)
			require.Contains(t, []string{"agent", "mcp"}, p.Type, "%s: bad type", p.Name)
			require.NotEmpty(t, p.Framework, "%s: framework required", p.Name)
			require.NotEmpty(t, p.Language, "%s: language required", p.Name)
			require.NotEmpty(t, p.Description, "%s: description required (shown in picker)", p.Name)
			require.True(t,
				len(p.Build.Command) > 0 || p.Build.Script != "",
				"%s: build must have command or script", p.Name)
			require.True(t,
				len(p.Run.Command) > 0 || p.Run.Script != "",
				"%s: run must have command or script", p.Name)
		})
	}
}
