package declarative_test

import (
	"bytes"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/cli/declarative"
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
// and strips server-managed fields so the output round-trips through apply.
func TestDeploymentGet_YAMLOutputRoundTrips(t *testing.T) {
	deployments := []models.Deployment{
		{
			ID: "aws-v1", ServerName: "summarizer", Version: "1.0.0",
			ProviderID: "my-aws", ResourceType: "agent", Status: "deployed",
			Origin:           "managed",
			Env:              map[string]string{"GOOGLE_API_KEY": "xxx"},
			ProviderMetadata: models.JSONObject{"remoteId": "secret-runtime-id"},
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
	// Envelope shape — apply expects these keys at the top.
	assert.Contains(t, got, "apiVersion: ar.dev/v1alpha1")
	assert.Contains(t, got, "kind: Deployment")
	assert.Contains(t, got, "name: summarizer")
	assert.Contains(t, got, "version: 1.0.0")
	// Declarative spec fields.
	assert.Contains(t, got, "providerId: my-aws")
	assert.Contains(t, got, "resourceType: agent")
	assert.Contains(t, got, "GOOGLE_API_KEY: xxx")
	// Server-managed fields must be stripped.
	assert.NotContains(t, got, "aws-v1", "spec must not leak the server-assigned id")
	assert.NotContains(t, got, "status:", "spec must not leak the runtime status")
	assert.NotContains(t, got, "origin:", "spec must not leak the managed/discovered origin")
	assert.NotContains(t, got, "providerMetadata:", "spec must not leak provider-runtime metadata")
	assert.NotContains(t, got, "deployedAt", "spec must not leak server timestamps")
	assert.NotContains(t, got, "updatedAt", "spec must not leak server timestamps")
}
