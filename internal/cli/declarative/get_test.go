package declarative_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/cli/declarative"
	"github.com/agentregistry-dev/agentregistry/internal/cli/scheme"
	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetCmd_RejectsUnknownType(t *testing.T) {
	declarative.SetAPIClient(nil)
	cmd := declarative.NewGetCmd()
	cmd.SetArgs([]string{"unknowntype"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.ErrorContains(t, err, "unknown kind")
}

func TestGetCmd_RequiresTypeArg(t *testing.T) {
	cmd := declarative.NewGetCmd()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestGetCmd_NoAPIClientErrors(t *testing.T) {
	declarative.SetAPIClient(nil)
	cmd := declarative.NewGetCmd()
	cmd.SetArgs([]string{"agents"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.ErrorContains(t, err, "API client not initialized")
}

// TestGetCmd_RegistryDrivenColumnLookup verifies the package-level scheme
// registry resolves declarative-known kinds (declarative's init() registered
// them at process start), so `arctl get agents` gets past kind validation
// and fails only at the API-client check.
func TestGetCmd_RegistryDrivenColumnLookup(t *testing.T) {
	k, err := scheme.Lookup("agents")
	require.NoError(t, err, "agents alias should resolve via declarative's init() registration")
	assert.NotEmpty(t, k.TableColumns, "expected TableColumns on the agent kind")

	declarative.SetAPIClient(nil)

	// Looking up a valid kind should get past kind validation and fail
	// only at "API client not initialized" — confirming the dispatch ran.
	cmd := declarative.NewGetCmd()
	cmd.SetArgs([]string{"agents"})
	err = cmd.Execute()
	require.Error(t, err)
	assert.ErrorContains(t, err, "API client not initialized",
		"should fail at API client check, not kind lookup")
}

// TestProvider_NoAllTagsSupport pins that Runtime — a mutable
// namespace/name object — is registered without ListTags /
// DeleteAllTags closures. The dispatch layer rejects --all-tags
// when those fields are nil, which is exactly the behavior we want for
// Runtime on this branch (its server store has no /tags endpoint).
func TestProvider_NoAllTagsSupport(t *testing.T) {
	k, err := scheme.Lookup("runtime")
	require.NoError(t, err)
	require.Nil(t, k.ListTags, "Runtime should not expose ListTags (mutable object kind)")
	require.Nil(t, k.DeleteAllTags, "Runtime should not expose DeleteAllTags (mutable object kind)")
}

// TestDeployment_NoAllTagsSupport is the symmetric assertion for
// Deployment — also a mutable namespace/name object. Already
// covered by TestGet_AllTags_DeploymentRejected at the CLI surface
// but pinning it at the registry shape level guards against an
// accidental ListTags wiring regression.
func TestDeployment_NoAllTagsSupport(t *testing.T) {
	k, err := scheme.Lookup("deployment")
	require.NoError(t, err)
	require.Nil(t, k.ListTags, "Deployment should not expose ListTags (mutable object kind)")
	require.Nil(t, k.DeleteAllTags, "Deployment should not expose DeleteAllTags (mutable object kind)")
}

// tagGetServer serves GET /v0/agents/{name}/{tag} (specific tag)
// and /v0/agents/{name} (latest), returning the configured
// envelope. capturedPaths records every served path so tests can assert
// the right endpoint was hit.
func tagGetServer(t *testing.T, latest, specific v1alpha1.Agent) (*httptest.Server, *[]string) {
	t.Helper()
	var (
		mu       sync.Mutex
		captured []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		captured = append(captured, r.Method+" "+r.URL.Path)
		mu.Unlock()
		if r.Method != http.MethodGet {
			http.Error(w, `{"error":"method"}`, http.StatusMethodNotAllowed)
			return
		}
		// /v0/agents/{name-escaped}/{tag} → specific
		// /v0/agents/{name-escaped}       → latest
		// Path comes in already URL-decoded for matching.
		w.Header().Set("Content-Type", "application/json")
		// Distinguish by the trailing path segment count.
		// Strip "/v0/agents/" prefix.
		if len(r.URL.Path) > len("/v0/agents/") {
			rest := r.URL.Path[len("/v0/agents/"):]
			// rest is e.g. "acme%2Fbot" (latest) or "acme%2Fbot/1" (specific).
			// Stdlib net/http decodes %2F back to "/" in r.URL.Path, so a name
			// "acme/bot" appears as literal slashes. We match on whether
			// there's an extra trailing segment beyond the name.
			// Easiest: count slashes in rest minus the name's slashes.
			// In our fixtures the agent name is "acme/bot" (one slash);
			// specific paths have two slashes.
			slashes := 0
			for i := 0; i < len(rest); i++ {
				if rest[i] == '/' {
					slashes++
				}
			}
			if slashes >= 2 {
				_ = json.NewEncoder(w).Encode(specific)
				return
			}
		}
		_ = json.NewEncoder(w).Encode(latest)
	}))
	t.Cleanup(srv.Close)
	return srv, &captured
}

// TestGet_Tag_FetchesSpecificTag verifies the --tag flag fetches the exact
// tag endpoint and renders that tag's envelope.
func TestGet_Tag_FetchesSpecificTag(t *testing.T) {
	v1 := agentTagFixture("acme/bot", "1")
	v2 := agentTagFixture("acme/bot", "2")
	srv, captured := tagGetServer(t, v2, v1)
	setupClientForServer(t, srv)

	out := &bytes.Buffer{}
	cmd := declarative.NewGetCmd()
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"agent", "acme/bot", "--tag", "1", "-o", "json"})
	require.NoError(t, cmd.Execute())

	var got v1alpha1.Agent
	require.NoError(t, json.Unmarshal(out.Bytes(), &got))
	assert.Equal(t, "1", got.Metadata.Tag, "expected tag 1 envelope")
	assert.Equal(t, "v1", got.Spec.Description, "expected v1's spec description")

	// At least one served call should be the exact-tag path.
	require.NotEmpty(t, *captured)
	hitSpecific := false
	for _, p := range *captured {
		// "GET /v0/agents/acme/bot/1" → 3 slashes after "/v0/agents/".
		if p == "GET /v0/agents/acme/bot/1" {
			hitSpecific = true
		}
	}
	assert.True(t, hitSpecific, "expected GET to exact-tag path, got %v", *captured)
}

// TestGet_Tag_DefaultsToLatest verifies that omitting --tag still
// hits the latest endpoint (no regression from --tag wiring).
func TestGet_Tag_DefaultsToLatest(t *testing.T) {
	v1 := agentTagFixture("acme/bot", "1")
	v2 := agentTagFixture("acme/bot", "2")
	srv, captured := tagGetServer(t, v2, v1)
	setupClientForServer(t, srv)

	out := &bytes.Buffer{}
	cmd := declarative.NewGetCmd()
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"agent", "acme/bot", "-o", "json"})
	require.NoError(t, cmd.Execute())

	var got v1alpha1.Agent
	require.NoError(t, json.Unmarshal(out.Bytes(), &got))
	assert.Equal(t, "2", got.Metadata.Tag, "expected latest tag 2 envelope")

	// All served calls should be the latest path (no tag segment).
	for _, p := range *captured {
		assert.Equal(t, "GET /v0/agents/acme/bot", p,
			"expected only latest-path GETs, got %v", *captured)
	}
}

// TestGet_Tag_MutuallyExclusiveWithAllTags pins the flag-validation
// guard on runGet.
func TestGet_Tag_MutuallyExclusiveWithAllTags(t *testing.T) {
	declarative.SetAPIClient(client.NewClient("http://127.0.0.1:1", ""))
	t.Cleanup(func() { declarative.SetAPIClient(nil) })

	cmd := declarative.NewGetCmd()
	cmd.SetArgs([]string{"agent", "acme/bot", "--tag", "1", "--all-tags"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

// TestGet_Tag_NotSupportedForProvider pins that --tag is rejected
// for mutable namespace/name kinds (Runtime, Deployment) before any client
// dispatch happens.
func TestGet_Tag_NotSupportedForProvider(t *testing.T) {
	declarative.SetAPIClient(client.NewClient("http://127.0.0.1:1", ""))
	t.Cleanup(func() { declarative.SetAPIClient(nil) })

	cmd := declarative.NewGetCmd()
	cmd.SetArgs([]string{"runtime", "my-kagent", "--tag", "1"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--tag not supported")
	assert.Contains(t, err.Error(), "runtime")
}

// TestGet_Tag_NotSupportedForDeployment is the symmetric assertion
// for Deployment.
func TestGet_Tag_NotSupportedForDeployment(t *testing.T) {
	declarative.SetAPIClient(client.NewClient("http://127.0.0.1:1", ""))
	t.Cleanup(func() { declarative.SetAPIClient(nil) })

	cmd := declarative.NewGetCmd()
	cmd.SetArgs([]string{"deployment", "summarizer", "--tag", "1"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--tag not supported")
	assert.Contains(t, err.Error(), "deployment")
}

// TestGet_Tag_ListModeFiltersByTag verifies that `arctl get agents --tag X`
// (no NAME) forwards `?tag=X` to the list endpoint. Earlier the CLI rejected
// the no-NAME form with "--tag requires NAME"; that constraint is gone now
// because the default list returns every tag, so --tag is the canonical way
// to scope a list to one tag value.
func TestGet_Tag_ListModeFiltersByTag(t *testing.T) {
	var (
		mu       sync.Mutex
		captured []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		captured = append(captured, r.URL.RawQuery)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	t.Cleanup(srv.Close)
	setupClientForServer(t, srv)

	cmd := declarative.NewGetCmd()
	cmd.SetArgs([]string{"agents", "--tag", "0.1.0"})
	require.NoError(t, cmd.Execute())

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, captured, "expected at least one server call")
	assert.Contains(t, captured[0], "tag=0.1.0",
		"expected ?tag=0.1.0 to flow through to the list query, got %q", captured[0])
}

// TestGet_Latest_ListModeFiltersByLatestOnly verifies `--latest` (no NAME)
// flips `?latestOnly=true` on the list query. This is the explicit re-opt
// into the old "default list filter" behavior that used to be implicit.
func TestGet_Latest_ListModeFiltersByLatestOnly(t *testing.T) {
	var (
		mu       sync.Mutex
		captured []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		captured = append(captured, r.URL.RawQuery)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	t.Cleanup(srv.Close)
	setupClientForServer(t, srv)

	cmd := declarative.NewGetCmd()
	cmd.SetArgs([]string{"agents", "--latest"})
	require.NoError(t, cmd.Execute())

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, captured, "expected at least one server call")
	assert.Contains(t, captured[0], "latestOnly=true",
		"expected ?latestOnly=true to flow through, got %q", captured[0])
}

// TestGet_ListModeDefault_NoTagFilter verifies the new default: a plain
// `arctl get agents` does NOT send tag= or latestOnly=, so the server
// returns every row. This is the contract that fixes the empty-list bug
// for resources published with explicit version tags.
func TestGet_ListModeDefault_NoTagFilter(t *testing.T) {
	var (
		mu       sync.Mutex
		captured []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		captured = append(captured, r.URL.RawQuery)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	t.Cleanup(srv.Close)
	setupClientForServer(t, srv)

	cmd := declarative.NewGetCmd()
	cmd.SetArgs([]string{"agents"})
	require.NoError(t, cmd.Execute())

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, captured, "expected at least one server call")
	assert.NotContains(t, captured[0], "tag=",
		"default list should not pass a tag filter, got %q", captured[0])
	assert.NotContains(t, captured[0], "latestOnly=true",
		"default list should not pass latestOnly, got %q", captured[0])
}

// TestGet_TagAndLatest_MutuallyExclusive pins the flag-validation guard.
func TestGet_TagAndLatest_MutuallyExclusive(t *testing.T) {
	declarative.SetAPIClient(client.NewClient("http://127.0.0.1:1", ""))
	t.Cleanup(func() { declarative.SetAPIClient(nil) })

	cmd := declarative.NewGetCmd()
	cmd.SetArgs([]string{"agents", "--tag", "1", "--latest"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

// TestGet_Latest_NotSupportedForProvider mirrors the --tag guard: --latest
// is also a tag-shaped filter and should be rejected for mutable kinds
// before any dispatch.
func TestGet_Latest_NotSupportedForProvider(t *testing.T) {
	declarative.SetAPIClient(client.NewClient("http://127.0.0.1:1", ""))
	t.Cleanup(func() { declarative.SetAPIClient(nil) })

	cmd := declarative.NewGetCmd()
	cmd.SetArgs([]string{"runtime", "--latest"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--latest not supported")
	assert.Contains(t, err.Error(), "runtime")
}

// TestGet_Tag_RejectsGetAll pins that --tag is rejected for
// `arctl get all` (cross-kind list flow).
func TestGet_Tag_RejectsGetAll(t *testing.T) {
	cmd := declarative.NewGetCmd()
	cmd.SetArgs([]string{"all", "--tag", "1"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--tag cannot be used with `get all`")
}

// TestGet_Latest_RejectsGetAll is the symmetric guard for --latest on
// cross-kind list.
func TestGet_Latest_RejectsGetAll(t *testing.T) {
	cmd := declarative.NewGetCmd()
	cmd.SetArgs([]string{"all", "--latest"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--latest cannot be used with `get all`")
}
