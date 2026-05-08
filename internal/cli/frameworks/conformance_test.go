package frameworks

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Every embedded framework must parse, type-check, and have at least a build
// command (or script). Catches malformed framework.yaml at compile time.
func TestEmbeddedFrameworks_ConformToContract(t *testing.T) {
	frameworks, err := LoadEmbedded(t.TempDir())
	require.NoError(t, err)
	require.NotEmpty(t, frameworks, "no embedded frameworks found — the in-tree set is empty")

	for _, p := range frameworks {
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
