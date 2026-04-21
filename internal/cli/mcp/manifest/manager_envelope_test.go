package manifest_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/cli/mcp/manifest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPManagerLoad_EnvelopeFormat(t *testing.T) {
	dir := t.TempDir()
	envelopeYAML := `apiVersion: ar.dev/v1alpha1
kind: MCPServer
metadata:
  name: acme/fetch
  version: "1.0.0"
spec:
  title: Fetch Server
  description: "Fetches content"
  packages:
    - registryType: oci
      identifier: ghcr.io/acme/fetch:1.0.0
      runtimeHint: docker
      transport:
        type: stdio
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mcp.yaml"), []byte(envelopeYAML), 0o644))

	m := manifest.NewManager(dir)
	got, err := m.Load()
	require.NoError(t, err)
	require.NotNil(t, got)

	assert.Equal(t, "acme/fetch", got.Name)
	assert.Equal(t, "1.0.0", got.Version)
	assert.Equal(t, "Fetches content", got.Description)
	assert.Equal(t, "docker", got.RuntimeHint)
}

func TestMCPManagerLoad_LegacyFlatFormat(t *testing.T) {
	dir := t.TempDir()
	flatYAML := `name: acme/legacy
framework: fastmcp-python
version: "1.0.0"
description: "Legacy flat mcp"
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mcp.yaml"), []byte(flatYAML), 0o644))

	m := manifest.NewManager(dir)
	got, err := m.Load()
	require.NoError(t, err)
	require.NotNil(t, got)

	assert.Equal(t, "acme/legacy", got.Name)
	assert.Equal(t, "fastmcp-python", got.Framework)
	assert.Equal(t, "1.0.0", got.Version)
}

func TestMCPManagerLoad_MalformedYAML(t *testing.T) {
	dir := t.TempDir()
	malformed := []byte("apiVersion: ar.dev/v1alpha1\nkind: MCPServer\nspec: [this is not a map")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mcp.yaml"), malformed, 0o644))

	m := manifest.NewManager(dir)
	_, err := m.Load()
	require.Error(t, err, "malformed yaml must surface an error, not a silent zero-value manifest")
}

func TestMCPManagerLoad_EnvelopeNoPackages(t *testing.T) {
	dir := t.TempDir()
	envelopeYAML := `apiVersion: ar.dev/v1alpha1
kind: MCPServer
metadata:
  name: acme/no-pkgs
  version: "1.0.0"
spec:
  title: No Packages
  description: "No packages in this spec"
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mcp.yaml"), []byte(envelopeYAML), 0o644))

	m := manifest.NewManager(dir)
	got, err := m.Load()
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "acme/no-pkgs", got.Name)
	assert.Empty(t, got.RuntimeHint, "RuntimeHint must be empty when no packages are present")
}
