package declarative

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRun_RemoteOnlyMCPProject_Errors(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "arctl.yaml"), []byte(`
framework: fastmcp
language: python
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mcp.yaml"), []byte(`
apiVersion: ar.dev/v1alpha1
kind: MCPServer
metadata:
  name: acme-remote-only
spec:
  remote:
    type: streamable-http
    url: https://example.com/mcp
`), 0o644))

	var buf bytes.Buffer
	err := runProject(context.Background(), &buf, dir, nil, true /*dryRun*/, false, false, false, false)
	require.Error(t, err, "remote-only mcp.yaml must yield a Run-B error")
	assert.Contains(t, err.Error(), "remote MCPServer")
	assert.Contains(t, err.Error(), "https://example.com/mcp")
	assert.Contains(t, err.Error(), "npx -y @modelcontextprotocol/inspector")
}
