package declarative_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/cli/declarative"
	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// agentVersionFixture builds a minimal Agent envelope at the given tag for use
// as a row in a /tags list response.
func agentVersionFixture(name, version string) v1alpha1.Agent {
	return v1alpha1.Agent{
		TypeMeta: v1alpha1.TypeMeta{
			APIVersion: v1alpha1.GroupVersion,
			Kind:       v1alpha1.KindAgent,
		},
		Metadata: v1alpha1.ObjectMeta{
			Namespace: v1alpha1.DefaultNamespace,
			Name:      name,
			Tag:       version,
		},
		Spec: v1alpha1.AgentSpec{
			Description: "v" + version,
		},
	}
}

// versionsListServer serves GET /v0/agents/{name}/tags and replies with
// the provided rows. Other endpoints respond 404 so unintended calls fail
// loudly. capturedPaths records every path served (concurrency-safe).
func versionsListServer(t *testing.T, rows []v1alpha1.Agent) (*httptest.Server, *[]string) {
	t.Helper()
	var (
		mu       sync.Mutex
		captured []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		captured = append(captured, r.Method+" "+r.URL.Path)
		mu.Unlock()
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/tags") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"items": rows})
			return
		}
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv, &captured
}

// (1) `arctl get agent NAME --all-versions` prints one row per version
// returned by the server.
func TestGet_AllVersions_Agent_PrintsAllRows(t *testing.T) {
	rows := []v1alpha1.Agent{
		agentVersionFixture("acme/bot", "2"),
		agentVersionFixture("acme/bot", "1"),
	}
	srv, _ := versionsListServer(t, rows)
	setupClientForServer(t, srv)

	out := &bytes.Buffer{}
	cmd := declarative.NewGetCmd()
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"agent", "acme/bot", "--all-versions"})
	require.NoError(t, cmd.Execute())

	got := out.String()
	// Table output should include the NAME column once per row plus
	// each integer version. Counting occurrences of the name pins
	// "two rows printed" rather than "two versions appeared anywhere
	// in any column".
	assert.Equal(t, 2, strings.Count(got, "acme/bot"), "expected two rows in:\n%s", got)
	for _, line := range []string{"acme/bot   2", "acme/bot   1"} {
		assert.Contains(t, got, line, "expected %q in output:\n%s", line, got)
	}
}

// (2) `arctl get agent NAME --all-versions -o json` emits a JSON array of
// envelopes — verifies the multi-row YAML/JSON path also works.
func TestGet_AllVersions_Agent_JSONOutput(t *testing.T) {
	rows := []v1alpha1.Agent{
		agentVersionFixture("acme/bot", "2"),
		agentVersionFixture("acme/bot", "1"),
	}
	srv, _ := versionsListServer(t, rows)
	setupClientForServer(t, srv)

	out := &bytes.Buffer{}
	cmd := declarative.NewGetCmd()
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"agent", "acme/bot", "--all-versions", "-o", "json"})
	require.NoError(t, cmd.Execute())

	var got []v1alpha1.Agent
	require.NoError(t, json.Unmarshal(out.Bytes(), &got))
	require.Len(t, got, 2)
	assert.Equal(t, "2", got[0].Metadata.Tag)
	assert.Equal(t, "1", got[1].Metadata.Tag)
}

// (3) `arctl get deployment NAME --all-versions` errors cleanly because
// deployments are mutable namespace/name objects, not taggable artifacts.
func TestGet_AllVersions_DeploymentRejected(t *testing.T) {
	declarative.SetAPIClient(client.NewClient("http://127.0.0.1:1", ""))
	t.Cleanup(func() { declarative.SetAPIClient(nil) })

	cmd := declarative.NewGetCmd()
	cmd.SetArgs([]string{"deployment", "summarizer", "--all-versions"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--all-versions not supported")
	assert.Contains(t, err.Error(), "deployment")
}

// (3b) `arctl get provider NAME --all-versions` errors cleanly — Provider
// is a mutable namespace/name object whose store has no /tags endpoint.
// Pin the CLI surface so a future typedKind change can't
// silently re-expose --all-versions for Provider.
func TestGet_AllVersions_ProviderRejected(t *testing.T) {
	declarative.SetAPIClient(client.NewClient("http://127.0.0.1:1", ""))
	t.Cleanup(func() { declarative.SetAPIClient(nil) })

	cmd := declarative.NewGetCmd()
	cmd.SetArgs([]string{"provider", "my-kagent", "--all-versions"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--all-versions not supported")
	assert.Contains(t, err.Error(), "provider")
}

// (4) `arctl get agents --all-versions` (no NAME) errors — the flag
// requires a NAME argument.
func TestGet_AllVersions_RequiresName(t *testing.T) {
	declarative.SetAPIClient(client.NewClient("http://127.0.0.1:1", ""))
	t.Cleanup(func() { declarative.SetAPIClient(nil) })

	cmd := declarative.NewGetCmd()
	cmd.SetArgs([]string{"agents", "--all-versions"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--all-versions requires NAME")
}

// (5) `arctl get all --all-versions` errors — the cross-kind list flow has
// no notion of "all tags of every name".
func TestGet_AllVersions_RejectsGetAll(t *testing.T) {
	cmd := declarative.NewGetCmd()
	cmd.SetArgs([]string{"all", "--all-versions"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--all-versions cannot be used with `get all`")
}

// deleteAllTagsServer serves GET /v0/agents/{name}/tags plus exact-tag DELETEs.
// capturedPaths records every served request for assertions.
func deleteAllTagsServer(t *testing.T, rows []v1alpha1.Agent, failTag string) (*httptest.Server, *[]string) {
	t.Helper()
	var (
		mu       sync.Mutex
		captured []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		captured = append(captured, r.Method+" "+r.URL.Path)
		mu.Unlock()
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/tags"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"items": rows})
		case r.Method == http.MethodDelete:
			if failTag != "" && strings.HasSuffix(r.URL.Path, "/"+failTag) {
				http.Error(w, "boom", http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &captured
}

// (6) `arctl delete agent NAME --all-versions` lists tags and deletes each
// exact tag so omitted-tag declarative delete can continue to mean "latest".
func TestDelete_AllVersions_Agent_DeletesEveryListedTag(t *testing.T) {
	rows := []v1alpha1.Agent{
		agentVersionFixture("acme/bot", "stable"),
		agentVersionFixture("acme/bot", "latest"),
	}
	srv, paths := deleteAllTagsServer(t, rows, "")
	setupClientForServer(t, srv)

	out := &bytes.Buffer{}
	cmd := declarative.NewDeleteCmd()
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"agent", "acme/bot", "--all-versions"})
	require.NoError(t, cmd.Execute())

	require.Contains(t, *paths, "GET /v0/agents/acme/bot/tags")
	require.Contains(t, *paths, "DELETE /v0/agents/acme/bot/stable")
	require.Contains(t, *paths, "DELETE /v0/agents/acme/bot/latest")
	assert.Contains(t, out.String(), "all tags")
}

// (7) `arctl delete deployment NAME --all-versions` errors cleanly.
func TestDelete_AllVersions_DeploymentRejected(t *testing.T) {
	declarative.SetAPIClient(client.NewClient("http://127.0.0.1:1", ""))
	t.Cleanup(func() { declarative.SetAPIClient(nil) })

	cmd := declarative.NewDeleteCmd()
	cmd.SetArgs([]string{"deployment", "summarizer", "--all-versions"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--all-versions not supported")
}

// (7b) `arctl delete provider NAME --all-versions` errors cleanly —
// Provider has no DeleteAllVersions endpoint server-side.
func TestDelete_AllVersions_ProviderRejected(t *testing.T) {
	declarative.SetAPIClient(client.NewClient("http://127.0.0.1:1", ""))
	t.Cleanup(func() { declarative.SetAPIClient(nil) })

	cmd := declarative.NewDeleteCmd()
	cmd.SetArgs([]string{"provider", "my-kagent", "--all-versions"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--all-versions not supported")
}

// (8) `arctl delete agent NAME --all-versions --version 1` errors — the
// flags are mutually exclusive.
func TestDelete_AllVersions_AndVersionMutuallyExclusive(t *testing.T) {
	cmd := declarative.NewDeleteCmd()
	cmd.SetArgs([]string{"agent", "acme/bot", "--all-versions", "--version", "1"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

// (9) An exact-tag delete failure is surfaced by the CLI.
func TestDelete_AllVersions_PropagatesServerFailure(t *testing.T) {
	rows := []v1alpha1.Agent{agentVersionFixture("acme/bot", "stable")}
	srv, _ := deleteAllTagsServer(t, rows, "stable")
	setupClientForServer(t, srv)

	cmd := declarative.NewDeleteCmd()
	cmd.SetArgs([]string{"agent", "acme/bot", "--all-versions"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}
