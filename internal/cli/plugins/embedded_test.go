package plugins

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// LoadEmbedded returns whatever plugins are baked into the binary. With no plugins
// shipped yet, the result is empty (but the call must succeed).
func TestLoadEmbedded_EmptyOK(t *testing.T) {
	plugins, err := LoadEmbedded(t.TempDir())
	require.NoError(t, err)
	assert.NotNil(t, plugins) // empty slice or nil, both fine
}
