package scheme_test

import (
	"encoding/json"
	"os"
	"sync"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/cli/scheme"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	registerOnce          sync.Once
	extensionRegisterOnce sync.Once
)

type extensionSpec struct {
	Value string `json:"value" yaml:"value"`
}

type extensionObject struct {
	v1alpha1.TypeMeta `json:",inline" yaml:",inline"`
	Metadata          v1alpha1.ObjectMeta `json:"metadata" yaml:"metadata"`
	Spec              extensionSpec       `json:"spec" yaml:"spec"`
	Status            v1alpha1.Status     `json:"status,omitzero" yaml:"status,omitempty"`
}

func (e *extensionObject) GetMetadata() *v1alpha1.ObjectMeta    { return &e.Metadata }
func (e *extensionObject) SetMetadata(meta v1alpha1.ObjectMeta) { e.Metadata = meta }
func (e *extensionObject) Validate() error                      { return nil }
func (e *extensionObject) MarshalSpec() (json.RawMessage, error) {
	return json.Marshal(e.Spec)
}
func (e *extensionObject) UnmarshalSpec(data json.RawMessage) error {
	return json.Unmarshal(data, &e.Spec)
}
func (e *extensionObject) GetStatus() *v1alpha1.Status      { return &e.Status }
func (e *extensionObject) SetStatus(status v1alpha1.Status) { e.Status = status }
func (e *extensionObject) MarshalStatus() (json.RawMessage, error) {
	return v1alpha1.MarshalStatusForStorage(e.Status)
}
func (e *extensionObject) UnmarshalStatus(data json.RawMessage) error {
	return v1alpha1.UnmarshalStatusFromStorage(data, &e.Status)
}

// TestMain registers the two kinds the tests below need against the
// scheme package-level table. The CLI's declarative package isn't
// imported here so its init() doesn't fire — these registrations stand
// alone for the scheme test binary.
func TestMain(m *testing.M) {
	registerOnce.Do(func() {
		scheme.Register(&scheme.Kind{
			Kind: "agent", Plural: "agents", Aliases: []string{"Agent"},
		})
		scheme.Register(&scheme.Kind{
			Kind: "mcp", Plural: "mcps",
			Aliases: []string{"MCPServer", "mcpserver", "mcpservers"},
		})
	})
	os.Exit(m.Run())
}

func TestDecodeBytesSingleDoc(t *testing.T) {
	input := `
apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  name: acme/bot
  version: "1.0.0"
spec:
  source:
    image: ghcr.io/acme/bot:latest
  description: "A bot"
`

	objs, err := scheme.DecodeBytes([]byte(input))
	require.NoError(t, err)
	require.Len(t, objs, 1)

	agent, ok := objs[0].(*v1alpha1.Agent)
	require.True(t, ok, "expected *v1alpha1.Agent, got %T", objs[0])
	assert.Equal(t, "ar.dev/v1alpha1", agent.GetAPIVersion())
	assert.Equal(t, "Agent", agent.GetKind())
	assert.Equal(t, "acme/bot", agent.Metadata.Name)
	assert.Equal(t, "1.0.0", agent.Metadata.Version)
	assert.Equal(t, "ghcr.io/acme/bot:latest", agent.Spec.Source.Image)
}

func TestDecodeBytesMultiDoc(t *testing.T) {
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
  source:
    image: ghcr.io/acme/bot:latest
`

	objs, err := scheme.DecodeBytes([]byte(input))
	require.NoError(t, err)
	require.Len(t, objs, 2)
	assert.Equal(t, "MCPServer", objs[0].GetKind())
	assert.Equal(t, "Agent", objs[1].GetKind())
}

func TestDecodeBytesAllowsSchemeRegisteredExtensionKind(t *testing.T) {
	const extensionKind = "SchemeTestExtension"
	extensionRegisterOnce.Do(func() {
		v1alpha1.Default.MustRegister(extensionKind, extensionSpec{}, func() any { return &extensionObject{} })
	})

	input := `
apiVersion: ar.dev/v1alpha1
kind: SchemeTestExtension
metadata:
  name: enterprise-only
  version: "1"
spec:
  value: ok
`
	objs, err := scheme.DecodeBytes([]byte(input))
	require.NoError(t, err)
	require.Len(t, objs, 1)
	assert.Equal(t, extensionKind, objs[0].GetKind())
}

func TestDecodeBytesMissingKind(t *testing.T) {
	input := `
apiVersion: ar.dev/v1alpha1
metadata:
  name: acme/bot
spec:
  source:
    image: ghcr.io/acme/bot:latest
`
	_, err := scheme.DecodeBytes([]byte(input))
	assert.ErrorContains(t, err, "kind")
}

func TestDecodeBytesUnknownKind(t *testing.T) {
	input := `
apiVersion: ar.dev/v1alpha1
kind: BogusKind
metadata:
  name: acme/bot
spec: {}
`
	_, err := scheme.DecodeBytes([]byte(input))
	require.Error(t, err)
	assert.ErrorContains(t, err, "BogusKind")
}

func TestDecodeBytesEmptyInput(t *testing.T) {
	objs, err := scheme.DecodeBytes([]byte(""))
	require.NoError(t, err)
	assert.Empty(t, objs)
}

// TestDecodeBytesDropsIncomingStatus pins the contract that the CLI
// decoder zeroes Status on every doc so `arctl get -o yaml | apply -f -`
// stays apply-safe even when the source carried server-managed status.
func TestDecodeBytesDropsIncomingStatus(t *testing.T) {
	input := `
apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  name: acme/bot
  version: "1.0.0"
spec:
  source:
    image: ghcr.io/acme/bot:latest
status:
  conditions:
    - type: Ready
      status: "True"
`

	objs, err := scheme.DecodeBytes([]byte(input))
	require.NoError(t, err)
	require.Len(t, objs, 1)

	agent, ok := objs[0].(*v1alpha1.Agent)
	require.True(t, ok)
	assert.Empty(t, agent.Status.Conditions)
}
