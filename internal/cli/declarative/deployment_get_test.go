package declarative_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/cli/declarative"
	"github.com/agentregistry-dev/agentregistry/internal/cli/scheme"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// (1) Get by name returns the matching deployment when exactly one exists.
func TestDeploymentGet_ReturnsMatchByName(t *testing.T) {
	deployments := []models.Deployment{
		{ID: "aws-v1", ServerName: "summarizer", Version: "1.0.0", ProviderID: "my-aws", ResourceType: "agent", Status: "deployed"},
		{ID: "other", ServerName: "unrelated", Version: "1.0.0", ProviderID: "my-aws", ResourceType: "agent", Status: "deployed"},
	}
	srv, _ := deploymentTestServer(t, deployments, nil)
	setupClientForServer(t, srv)

	out := &bytes.Buffer{}
	cmd := declarative.NewGetCmd()
	cmd.SetOut(out)
	cmd.SetArgs([]string{"deployment", "summarizer"})
	require.NoError(t, cmd.Execute())

	assert.Contains(t, out.String(), "summarizer",
		"get should render the matching deployment's name in the table output")
	assert.NotContains(t, out.String(), "unrelated",
		"unrelated deployments must not appear")
}

// (2) Get returns the first match when multiple deployments share a name.
// Users needing disambiguation should use `arctl get deployments`.
func TestDeploymentGet_ReturnsFirstWhenMultipleShareName(t *testing.T) {
	deployments := []models.Deployment{
		{ID: "aws-v1", ServerName: "summarizer", Version: "1.0.0", ProviderID: "my-aws", ResourceType: "agent", Status: "deployed"},
		{ID: "gcp-v1", ServerName: "summarizer", Version: "1.0.0", ProviderID: "my-gcp", ResourceType: "agent", Status: "deployed"},
		{ID: "aws-v2", ServerName: "summarizer", Version: "2.0.0", ProviderID: "my-aws", ResourceType: "agent", Status: "deployed"},
	}
	srv, _ := deploymentTestServer(t, deployments, nil)
	setupClientForServer(t, srv)

	out := &bytes.Buffer{}
	cmd := declarative.NewGetCmd()
	cmd.SetOut(out)
	cmd.SetArgs([]string{"deployment", "summarizer"})
	require.NoError(t, cmd.Execute())

	// First match by list order is aws-v1; output should include its ID, not the others.
	assert.Contains(t, out.String(), "aws-v1",
		"first deployment for the name should be returned")
	assert.NotContains(t, out.String(), "gcp-v1",
		"only the first match is surfaced; subsequent matches are filtered out")
	assert.NotContains(t, out.String(), "aws-v2",
		"other versions must not be surfaced when get returns first match")
}

// (3) Get surfaces the registry's not-found sentinel when no deployment matches.
// This mirrors other kinds (agent / mcp / skill / prompt) — the CLI wraps the
// sentinel so tooling can still distinguish "not found" from transport failures.
func TestDeploymentGet_NotFoundError(t *testing.T) {
	deployments := []models.Deployment{
		{ID: "other", ServerName: "unrelated", Version: "1.0.0", ProviderID: "my-aws", ResourceType: "agent", Status: "deployed"},
	}
	srv, _ := deploymentTestServer(t, deployments, nil)
	setupClientForServer(t, srv)

	out := &bytes.Buffer{}
	cmd := declarative.NewGetCmd()
	cmd.SetOut(out)
	cmd.SetArgs([]string{"deployment", "does-not-exist"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found",
		"missing deployment should surface a not-found error")
}

// (4) List mode (no name arg) returns every deployment — exercises the shared
// ListFunc path and guards against the Get wiring accidentally short-circuiting list.
func TestDeploymentGet_ListReturnsAll(t *testing.T) {
	deployments := []models.Deployment{
		{ID: "aws-v1", ServerName: "summarizer", Version: "1.0.0", ProviderID: "my-aws", ResourceType: "agent", Status: "deployed"},
		{ID: "gcp-v1", ServerName: "other", Version: "1.0.0", ProviderID: "my-gcp", ResourceType: "agent", Status: "pending"},
	}
	srv, _ := deploymentTestServer(t, deployments, nil)
	setupClientForServer(t, srv)

	out := &bytes.Buffer{}
	cmd := declarative.NewGetCmd()
	cmd.SetOut(out)
	cmd.SetArgs([]string{"deployments"})
	require.NoError(t, cmd.Execute())

	assert.Contains(t, out.String(), "summarizer")
	assert.Contains(t, out.String(), "other")
}

// (5) `-o yaml` emits the declarative envelope (apiVersion/kind/metadata/spec)
// plus a .status block with server-managed runtime state. Spec itself stays
// clean so `get -o yaml | apply -f -` round-trips without leaking server
// fields back into the stored spec.
func TestDeploymentGet_YAMLOutputIncludesStatus(t *testing.T) {
	deployments := []models.Deployment{
		{
			ID: "aws-v1", ServerName: "summarizer", Version: "1.0.0",
			ProviderID: "my-aws", ResourceType: "agent", Status: "deployed",
			Origin:           "managed",
			Env:              map[string]string{"GOOGLE_API_KEY": "xxx"},
			ProviderMetadata: models.JSONObject{"remoteId": "runtime-abc"},
			Error:            "",
		},
	}
	srv, _ := deploymentTestServer(t, deployments, nil)
	setupClientForServer(t, srv)

	out := &bytes.Buffer{}
	cmd := declarative.NewGetCmd()
	cmd.SetOut(out)
	cmd.SetArgs([]string{"deployment", "summarizer", "-o", "yaml"})
	require.NoError(t, cmd.Execute())

	got := out.String()

	// Envelope — apply expects these top-level keys.
	assert.Contains(t, got, "apiVersion: ar.dev/v1alpha1")
	assert.Contains(t, got, "kind: Deployment")
	assert.Contains(t, got, "name: summarizer")
	assert.Contains(t, got, "version: 1.0.0")

	// Spec block — declarative fields only.
	assert.Contains(t, got, "providerId: my-aws")
	assert.Contains(t, got, "resourceType: agent")
	assert.Contains(t, got, "GOOGLE_API_KEY: xxx")

	// Status block — server-managed runtime state, available for debugging.
	assert.Contains(t, got, "status:")
	assert.Contains(t, got, "id: aws-v1")
	assert.Contains(t, got, "phase: deployed")
	assert.Contains(t, got, "origin: managed")
	assert.Contains(t, got, "remoteId: runtime-abc",
		"providerMetadata nested map should be emitted under .status")

	// Spec block still must NOT contain status fields (structural check:
	// the line immediately following `spec:` must not be the status keys).
	// Use a regex over the whole output: spec: ... must not contain id:/phase: until status: is hit.
	specIdx := strings.Index(got, "spec:")
	statusIdx := strings.Index(got, "status:")
	require.Positive(t, specIdx)
	require.Greater(t, statusIdx, specIdx, "status must come after spec")
	specBlock := got[specIdx:statusIdx]
	assert.NotContains(t, specBlock, "id: aws-v1", "server id must not leak into spec")
	assert.NotContains(t, specBlock, "phase:", "phase must not leak into spec")
	assert.NotContains(t, specBlock, "origin:", "origin must not leak into spec")
}

// (6) Apply must silently ignore a .status block on input YAML. This is the
// round-trip guarantee: `arctl get deployment X -o yaml | arctl apply -f -`
// should produce the same spec without the incoming status leaking into the
// stored record.
func TestDeploymentApply_IgnoresIncomingStatus(t *testing.T) {
	// The envelope decoder at internal/registry/kinds/registry.go:decodeNode
	// unmarshals only apiVersion/kind/metadata/spec. The status field on
	// Document is marshal-only from the server's perspective. This test
	// guards that contract: round-tripping a status-bearing document through
	// YAML decode produces zero-value Status.
	raw := []byte(`apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  name: myagent
  version: "1.0.0"
spec:
  image: ghcr.io/example/myagent:1.0.0
  language: python
  framework: adk
status:
  phase: deployed
  id: some-runtime-id
  deployedAt: 2026-04-20T10:00:00Z
`)

	reg := declarative.NewCLIRegistry()
	docs, err := scheme.DecodeBytes(reg, raw)
	require.NoError(t, err, "apply decode must tolerate incoming status block")
	require.Len(t, docs, 1)

	assert.Equal(t, "agent", docs[0].Kind, "decoder canonicalizes Kind to lowercase")
	assert.Equal(t, "myagent", docs[0].Metadata.Name)
	assert.Nil(t, docs[0].Status, "status block on input must be dropped, not preserved")
}
