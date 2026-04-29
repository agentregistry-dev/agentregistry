package declarative_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/cli/declarative"
	"github.com/agentregistry-dev/agentregistry/internal/cli/scheme"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func deploymentFixture(metaName, targetName, version, providerID, resourceType, phase string) v1alpha1.Deployment {
	targetKind := v1alpha1.KindAgent
	if resourceType == "mcp" {
		targetKind = v1alpha1.KindMCPServer
	}

	dep := v1alpha1.Deployment{
		TypeMeta: v1alpha1.TypeMeta{
			APIVersion: v1alpha1.GroupVersion,
			Kind:       v1alpha1.KindDeployment,
		},
		Metadata: v1alpha1.ObjectMeta{
			Namespace: v1alpha1.DefaultNamespace,
			Name:      metaName,
			Version:   version,
		},
		Spec: v1alpha1.DeploymentSpec{
			TargetRef: v1alpha1.ResourceRef{
				Kind:      targetKind,
				Namespace: v1alpha1.DefaultNamespace,
				Name:      targetName,
				Version:   version,
			},
			ProviderRef: v1alpha1.ResourceRef{
				Kind:      v1alpha1.KindProvider,
				Namespace: v1alpha1.DefaultNamespace,
				Name:      providerID,
			},
			DesiredState: v1alpha1.DesiredStateDeployed,
		},
	}
	switch phase {
	case "deployed":
		dep.Status.SetCondition(v1alpha1.Condition{Type: "Ready", Status: v1alpha1.ConditionTrue})
	case "failed":
		dep.Status.SetCondition(v1alpha1.Condition{Type: "Degraded", Status: v1alpha1.ConditionTrue, Message: "failed"})
	case "deploying":
		dep.Status.SetCondition(v1alpha1.Condition{Type: "Progressing", Status: v1alpha1.ConditionTrue})
	}
	return dep
}

func deploymentTestServerV1Alpha1(t *testing.T, deployments []v1alpha1.Deployment) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v0/deployments", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": deployments})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// (1) Get by name returns the matching deployment when exactly one exists.
func TestDeploymentGet_ReturnsMatchByName(t *testing.T) {
	deployments := []v1alpha1.Deployment{
		deploymentFixture("aws-v1", "summarizer", "1.0.0", "my-aws", "agent", "deployed"),
		deploymentFixture("other", "unrelated", "1.0.0", "my-aws", "agent", "deployed"),
	}
	srv := deploymentTestServerV1Alpha1(t, deployments)
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
	deployments := []v1alpha1.Deployment{
		deploymentFixture("aws-v1", "summarizer", "1.0.0", "my-aws", "agent", "deployed"),
		deploymentFixture("gcp-v1", "summarizer", "1.0.0", "my-gcp", "agent", "deployed"),
		deploymentFixture("aws-v2", "summarizer", "2.0.0", "my-aws", "agent", "deployed"),
	}
	srv := deploymentTestServerV1Alpha1(t, deployments)
	setupClientForServer(t, srv)

	out := &bytes.Buffer{}
	cmd := declarative.NewGetCmd()
	cmd.SetOut(out)
	cmd.SetArgs([]string{"deployment", "summarizer"})
	require.NoError(t, cmd.Execute())

	// First match by list order is aws-v1; output should include its ID, not the others.
	assert.Contains(t, out.String(), "default/aws-v1/1.0.0",
		"first deployment for the name should be returned")
	assert.NotContains(t, out.String(), "default/gcp-v1/1.0.0",
		"only the first match is surfaced; subsequent matches are filtered out")
	assert.NotContains(t, out.String(), "default/aws-v2/2.0.0",
		"other versions must not be surfaced when get returns first match")
}

// (3) Get surfaces the registry's not-found sentinel when no deployment matches.
// This mirrors other kinds (agent / mcp / skill / prompt) — the CLI wraps the
// sentinel so tooling can still distinguish "not found" from transport failures.
func TestDeploymentGet_NotFoundError(t *testing.T) {
	deployments := []v1alpha1.Deployment{
		deploymentFixture("other", "unrelated", "1.0.0", "my-aws", "agent", "deployed"),
	}
	srv := deploymentTestServerV1Alpha1(t, deployments)
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
	deployments := []v1alpha1.Deployment{
		deploymentFixture("aws-v1", "summarizer", "1.0.0", "my-aws", "agent", "deployed"),
		deploymentFixture("gcp-v1", "other", "1.0.0", "my-gcp", "agent", "pending"),
	}
	srv := deploymentTestServerV1Alpha1(t, deployments)
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
	deployment := deploymentFixture("aws-v1", "summarizer", "1.0.0", "my-aws", "agent", "deployed")
	deployment.Spec.Env = map[string]string{"GOOGLE_API_KEY": "xxx"}
	deployment.Metadata.Annotations = map[string]string{
		"platforms.agentregistry.solo.io/remoteId": "runtime-abc",
	}
	srv := deploymentTestServerV1Alpha1(t, []v1alpha1.Deployment{deployment})
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
	assert.Contains(t, got, "providerRef:")
	assert.Contains(t, got, "kind: Provider")
	assert.Contains(t, got, "name: my-aws")
	assert.Contains(t, got, "targetRef:")
	assert.Contains(t, got, "kind: Agent")
	assert.Contains(t, got, "GOOGLE_API_KEY: xxx")

	// Status block — server-managed runtime state, available for debugging.
	assert.Contains(t, got, "status:")
	assert.Contains(t, got, "id: default/aws-v1/1.0.0")
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
	assert.NotContains(t, specBlock, "id: default/aws-v1/1.0.0", "server id must not leak into spec")
	assert.NotContains(t, specBlock, "phase:", "phase must not leak into spec")
	assert.NotContains(t, specBlock, "origin:", "origin must not leak into spec")
}

// (6) Apply must silently ignore a .status block on input YAML. This is the
// round-trip guarantee: `arctl get deployment X -o yaml | arctl apply -f -`
// should produce the same spec without the incoming status leaking into the
// stored record.
func TestDeploymentApply_IgnoresIncomingStatus(t *testing.T) {
	// The v1alpha1 envelope decoder unmarshals only apiVersion/kind/metadata/spec
	// on apply input. Status is server-owned and marshal-only from the API's
	// perspective. This test guards that contract: round-tripping a
	// status-bearing document through YAML decode produces zero-value Status.
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

	docs, err := scheme.DecodeBytes(raw)
	require.NoError(t, err, "apply decode must tolerate incoming status block")
	require.Len(t, docs, 1)

	agent, ok := docs[0].(*v1alpha1.Agent)
	require.True(t, ok, "expected *v1alpha1.Agent, got %T", docs[0])
	assert.Equal(t, "Agent", agent.GetKind(), "decoder preserves the canonical envelope Kind from the YAML")
	assert.Equal(t, "myagent", agent.Metadata.Name)
	assert.Empty(t, agent.Status.Conditions, "status block on input must be dropped, not preserved")
}
