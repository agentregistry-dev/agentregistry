package scheme_test

import (
	"reflect"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/cli/scheme"
	"github.com/agentregistry-dev/agentregistry/internal/registry/kinds"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// syntheticSpec is the typed spec used in scheme tests.
type syntheticSpec struct {
	Image       string `yaml:"image"`
	Description string `yaml:"description"`
}

// newTestRegistry returns a registry with Agent and MCPServer kinds registered
// using syntheticSpec so tests do not depend on real service implementations.
func newTestRegistry() *kinds.Registry {
	reg := kinds.NewRegistry()
	reg.Register(kinds.Kind{
		Kind:     "agent",
		Plural:   "agents",
		Aliases:  []string{"Agent"},
		SpecType: reflect.TypeFor[syntheticSpec](),
	})
	reg.Register(kinds.Kind{
		Kind:     "mcpserver",
		Plural:   "mcpservers",
		Aliases:  []string{"MCPServer"},
		SpecType: reflect.TypeFor[syntheticSpec](),
	})
	return reg
}

func TestDecodeBytes_SingleDoc(t *testing.T) {
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

	spec, ok := resources[0].Spec.(*syntheticSpec)
	require.True(t, ok, "expected *syntheticSpec, got %T", resources[0].Spec)
	assert.Equal(t, "ghcr.io/acme/bot:latest", spec.Image)
}

func TestDecodeBytes_MultiDoc(t *testing.T) {
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
	assert.Equal(t, "mcpserver", resources[0].Kind)
	assert.Equal(t, "agent", resources[1].Kind)
}

func TestDecodeBytes_MissingKind(t *testing.T) {
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

func TestDecodeBytes_UnknownKind(t *testing.T) {
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
	assert.Error(t, err, "expected error for unknown kind")
}

func TestDecodeBytes_EmptyInput(t *testing.T) {
	reg := newTestRegistry()
	docs, err := scheme.DecodeBytes(reg, []byte(""))
	require.NoError(t, err)
	assert.Empty(t, docs)
}

func TestDecodeBytes_MetadataNamePreserved(t *testing.T) {
	reg := newTestRegistry()
	input := `
apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  name: acme/bot
  version: "1.0.0"
spec:
  image: ghcr.io/acme/bot:latest
`
	resources, err := scheme.DecodeBytes(reg, []byte(input))
	require.NoError(t, err)
	require.Len(t, resources, 1)
	assert.Equal(t, "acme/bot", resources[0].Metadata.Name)
	assert.Equal(t, "1.0.0", resources[0].Metadata.Version)
}
