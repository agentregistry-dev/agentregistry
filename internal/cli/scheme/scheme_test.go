package scheme_test

import (
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/cli/scheme"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestRegistry() *scheme.Registry {
	reg := scheme.NewRegistry()
	reg.Register(scheme.Kind{
		Kind:    "agent",
		Plural:  "agents",
		Aliases: []string{"Agent"},
	})
	reg.Register(scheme.Kind{
		Kind:    "mcp",
		Plural:  "mcps",
		Aliases: []string{"MCPServer", "mcpserver", "mcpservers"},
	})
	return reg
}

func TestDecodeBytesSingleDoc(t *testing.T) {
	reg := newTestRegistry()
	input := `
apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  name: acme/bot
  version: "1.0.0"
spec:
  image: ghcr.io/acme/bot:latest
  description: "A bot"
`

	resources, err := scheme.DecodeBytes(reg, []byte(input))
	require.NoError(t, err)
	require.Len(t, resources, 1)
	assert.Equal(t, "ar.dev/v1alpha1", resources[0].APIVersion)
	assert.Equal(t, "agent", resources[0].Kind)
	assert.Equal(t, "acme/bot", resources[0].Metadata.Name)
	assert.Equal(t, "1.0.0", resources[0].Metadata.Version)

	spec, ok := resources[0].Spec.(*v1alpha1.AgentSpec)
	require.True(t, ok, "expected *v1alpha1.AgentSpec, got %T", resources[0].Spec)
	assert.Equal(t, "ghcr.io/acme/bot:latest", spec.Image)
	assert.Nil(t, resources[0].Status)
}

func TestDecodeBytesMultiDoc(t *testing.T) {
	reg := newTestRegistry()
	input := `
apiVersion: ar.dev/v1alpha1
kind: MCPServer
metadata:
  name: acme/fetch
  version: "1.0.0"
spec:
  description: "Fetches URLs"
---
apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  name: acme/bot
  version: "1.0.0"
spec:
  description: "A bot"
  image: ghcr.io/acme/bot:latest
`

	resources, err := scheme.DecodeBytes(reg, []byte(input))
	require.NoError(t, err)
	require.Len(t, resources, 2)
	assert.Equal(t, "mcp", resources[0].Kind)
	assert.Equal(t, "agent", resources[1].Kind)
}

func TestDecodeBytesMissingKind(t *testing.T) {
	reg := newTestRegistry()
	input := `
apiVersion: ar.dev/v1alpha1
metadata:
  name: acme/bot
spec:
  image: ghcr.io/acme/bot:latest
`
	_, err := scheme.DecodeBytes(reg, []byte(input))
	assert.ErrorContains(t, err, "kind")
}

func TestDecodeBytesUnknownKind(t *testing.T) {
	reg := newTestRegistry()
	input := `
apiVersion: ar.dev/v1alpha1
kind: BogusKind
metadata:
  name: acme/bot
spec: {}
`
	_, err := scheme.DecodeBytes(reg, []byte(input))
	require.Error(t, err)
	assert.ErrorContains(t, err, "BogusKind")
}

func TestDecodeBytesEmptyInput(t *testing.T) {
	reg := newTestRegistry()
	docs, err := scheme.DecodeBytes(reg, []byte(""))
	require.NoError(t, err)
	assert.Empty(t, docs)
}

func TestDecodeBytesDropsIncomingStatus(t *testing.T) {
	reg := newTestRegistry()
	input := `
apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  name: acme/bot
  version: "1.0.0"
spec:
  image: ghcr.io/acme/bot:latest
status:
  conditions:
    - type: Ready
      status: "True"
`

	resources, err := scheme.DecodeBytes(reg, []byte(input))
	require.NoError(t, err)
	require.Len(t, resources, 1)
	assert.Nil(t, resources[0].Status)

	agent, ok := resources[0].Object.(*v1alpha1.Agent)
	require.True(t, ok)
	assert.Empty(t, agent.Status.Conditions)
}
