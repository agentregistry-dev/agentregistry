package scheme_test

import (
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/cli/scheme"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeBytes_SingleDoc(t *testing.T) {
	input := `
apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  name: acme/bot
  version: "1.0.0"
spec:
  image: ghcr.io/acme/bot:latest
  description: "A bot"
  language: python
  framework: adk
  modelProvider: google
  modelName: gemini-2.0-flash
`
	resources, err := scheme.DecodeBytes([]byte(input))
	require.NoError(t, err)
	require.Len(t, resources, 1)
	assert.Equal(t, "ar.dev/v1alpha1", resources[0].APIVersion)
	assert.Equal(t, "Agent", resources[0].Kind)
	assert.Equal(t, "acme/bot", resources[0].Metadata.Name)
	assert.Equal(t, "1.0.0", resources[0].Metadata.Version)
	assert.Equal(t, "ghcr.io/acme/bot:latest", resources[0].Spec["image"])
}

func TestDecodeBytes_MultiDoc(t *testing.T) {
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
  language: python
  framework: adk
  modelProvider: google
  modelName: gemini-2.0-flash
`
	resources, err := scheme.DecodeBytes([]byte(input))
	require.NoError(t, err)
	require.Len(t, resources, 2)
	assert.Equal(t, "MCPServer", resources[0].Kind)
	assert.Equal(t, "Agent", resources[1].Kind)
}

func TestDecodeBytes_MissingKind(t *testing.T) {
	input := `
apiVersion: ar.dev/v1alpha1
metadata:
  name: acme/bot
  version: "1.0.0"
spec:
  image: ghcr.io/acme/bot:latest
`
	_, err := scheme.DecodeBytes([]byte(input))
	assert.ErrorContains(t, err, "kind")
}

func TestDecodeBytes_MissingName(t *testing.T) {
	input := `
apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  version: "1.0.0"
spec:
  image: ghcr.io/acme/bot:latest
`
	_, err := scheme.DecodeBytes([]byte(input))
	assert.ErrorContains(t, err, "metadata.name")
}

func TestDecodeBytes_WrongAPIVersion(t *testing.T) {
	input := `
apiVersion: v1
kind: Agent
metadata:
  name: acme/bot
  version: "1.0.0"
spec: {}
`
	_, err := scheme.DecodeBytes([]byte(input))
	assert.ErrorContains(t, err, "apiVersion")
}

func TestDecodeBytes_EmptyInput(t *testing.T) {
	_, err := scheme.DecodeBytes([]byte(""))
	assert.ErrorContains(t, err, "no resources")
}
