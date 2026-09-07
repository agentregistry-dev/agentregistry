package commands_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/internal/cli/commands"
	"github.com/agentregistry-dev/agentregistry/internal/cli/scheme"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	cliruntime "github.com/agentregistry-dev/agentregistry/pkg/cli/runtime"
)

func TestGetCmd_RejectsUnknownType(t *testing.T) {
	cmd := commands.NewGetCmd(declarativeTestDeps(nil))
	cmd.SetArgs([]string{"unknowntype"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.ErrorContains(t, err, "unknown kind")
}

func TestGetCmd_RequiresTypeArg(t *testing.T) {
	cmd := commands.NewGetCmd(declarativeTestDeps(nil))
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestGetCmd_NoAPIClientErrors(t *testing.T) {
	cmd := commands.NewGetCmd(cliruntime.Deps{})
	cmd.SetArgs([]string{"agents"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.ErrorContains(t, err, "registry runtime not configured")
}

func TestGet_Labels_ListModeForwardsSelector(t *testing.T) {
	for _, flag := range []string{"--labels", "-l"} {
		t.Run(flag, func(t *testing.T) {
			var got string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.URL.Query().Get("labels")
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"items":[]}`))
			}))
			t.Cleanup(srv.Close)
			deps := setupClientForServer(t, srv)

			cmd := commands.NewGetCmd(deps)
			cmd.SetArgs([]string{"agents", flag, "team=platform,tier=production"})
			require.NoError(t, cmd.Execute())
			assert.Equal(t, "team=platform,tier=production", got)
		})
	}
}

func TestGet_Labels_RejectsNonListUses(t *testing.T) {

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "named resource", args: []string{"agent", "acme-bot", "-l", "team=platform"}, want: "cannot be combined with a resource NAME"},
		{name: "all tags", args: []string{"agent", "acme-bot", "--all-tags", "-l", "team=platform"}, want: "mutually exclusive"},
		{name: "get all", args: []string{"all", "-l", "team=platform"}, want: "cannot be used with `get all`"},
		{name: "non-content kind", args: []string{"runtime", "-l", "team=platform"}, want: "not supported for kind \"runtime\""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := commands.NewGetCmd(declarativeTestDeps(nil))
			cmd.SetArgs(tc.args)
			err := cmd.Execute()
			require.Error(t, err)
			assert.ErrorContains(t, err, tc.want)
		})
	}
}

func TestGet_ShowLabels(t *testing.T) {
	rows := []v1alpha1.Agent{
		func() v1alpha1.Agent {
			a := agentTagFixture("acme-bot", "latest")
			a.Metadata.Labels = map[string]string{"tier": "production", "team": "platform"}
			return a
		}(),
		agentTagFixture("beta-bot", "latest"),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": rows})
	}))
	t.Cleanup(srv.Close)
	deps := setupClientForServer(t, srv)

	out := &bytes.Buffer{}
	cmd := commands.NewGetCmd(deps)
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"agents", "--show-labels"})
	require.NoError(t, cmd.Execute())

	got := out.String()
	assert.Contains(t, got, "LABELS")
	assert.Contains(t, got, "team=platform,tier=production")
	assert.Contains(t, got, "<none>")
}

func TestGet_ShowLabels_OmittedByDefault(t *testing.T) {
	rows := []v1alpha1.Agent{agentTagFixture("acme-bot", "latest")}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": rows})
	}))
	t.Cleanup(srv.Close)
	deps := setupClientForServer(t, srv)

	out := &bytes.Buffer{}
	cmd := commands.NewGetCmd(deps)
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"agents"})
	require.NoError(t, cmd.Execute())

	assert.NotContains(t, out.String(), "LABELS")
}

// TestGetCmd_RegistryDrivenColumnLookup verifies the package-level scheme
// registry resolves declarative-known kinds (declarative's init() registered
// them at process start), so `arctl get agents` gets past kind validation
// and fails only at the API-client check.
func TestGetCmd_RegistryDrivenColumnLookup(t *testing.T) {
	k, err := scheme.Lookup("agents")
	require.NoError(t, err, "agents alias should resolve via declarative's init() registration")
	assert.Equal(t, []scheme.Column{
		{Header: "NAME"}, {Header: "TAG"}, {Header: "MODE"}, {Header: "DESCRIPTION"},
	}, k.TableColumns)

	// Looking up a valid kind should get past kind validation and fail
	// only at runtime setup — confirming the dispatch ran.
	cmd := commands.NewGetCmd(cliruntime.Deps{})
	cmd.SetArgs([]string{"agents"})
	err = cmd.Execute()
	require.Error(t, err)
	assert.ErrorContains(t, err, "registry runtime not configured",
		"should fail at API client check, not kind lookup")
}

func TestSecret_CLIRegistration(t *testing.T) {
	for _, alias := range []string{"secret", "secrets", "Secret"} {
		k, err := scheme.Lookup(alias)
		require.NoError(t, err, "%q should resolve via declarative's init() registration", alias)
		require.Equal(t, "secret", k.Kind)
		require.Equal(t, "secrets", k.Plural)
		require.Equal(t, []scheme.Column{
			{Header: "NAME"}, {Header: "TYPE"}, {Header: "KEYS"}, {Header: "IMMUTABLE"},
		}, k.TableColumns)
	}
}

func TestCommandHelpListsRegisteredKinds(t *testing.T) {
	getCmd := commands.NewGetCmd(declarativeTestDeps(nil))
	deleteCmd := commands.NewDeleteCmd(declarativeTestDeps(nil))
	for _, kind := range scheme.All() {
		t.Run(kind.Kind, func(t *testing.T) {
			require.NotEmpty(t, kind.Plural)
			assert.Contains(t, getCmd.Long, kind.Plural)
			assert.Contains(t, deleteCmd.Long, kind.Plural)
		})
	}
}

func TestEveryAPIKindHasCLIRegistration(t *testing.T) {
	for _, descriptor := range v1alpha1.KindDescriptors() {
		t.Run(descriptor.Kind, func(t *testing.T) {
			kind, err := scheme.Lookup(descriptor.Kind)
			require.NoError(t, err, "API kind %q is missing CLI registration", descriptor.Kind)
			require.NotEmpty(t, kind.Plural)
			require.NotEmpty(t, kind.TableColumns)
			require.NotNil(t, kind.RowFunc)
			require.NotNil(t, kind.ToYAMLFunc)
			require.NotNil(t, kind.Get)
			require.NotNil(t, kind.ListFunc)
			require.NotNil(t, kind.Delete)

			row := kind.RowFunc(descriptor.NewObject())
			require.Len(t, row, len(kind.TableColumns))

			switch descriptor.Storage {
			case v1alpha1.KindStorageTaggedArtifact:
				require.NotNil(t, kind.ListTags)
				require.NotNil(t, kind.DeleteAllTags)
			case v1alpha1.KindStorageMutableObject:
				require.Nil(t, kind.ListTags)
				require.Nil(t, kind.DeleteAllTags)
			default:
				t.Fatalf("unsupported storage type %q", descriptor.Storage)
			}
		})
	}
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
		path := r.URL.Path
		if r.URL.RawQuery != "" {
			path += "?" + r.URL.RawQuery
		}
		mu.Lock()
		captured = append(captured, r.Method+" "+path)
		mu.Unlock()
		if r.Method != http.MethodGet {
			http.Error(w, `{"error":"method"}`, http.StatusMethodNotAllowed)
			return
		}
		// /v0/agents/{name-escaped}/{tag} → specific
		// /v0/agents/{name-escaped}       → latest
		w.Header().Set("Content-Type", "application/json")
		// /v0/agents/<name>       → latest
		// /v0/agents/<name>/<tag> → specific
		if strings.Count(r.URL.Path[len("/v0/agents/"):], "/") >= 1 {
			_ = json.NewEncoder(w).Encode(specific)
			return
		}
		_ = json.NewEncoder(w).Encode(latest)
	}))
	t.Cleanup(srv.Close)
	return srv, &captured
}

// TestGet_Tag_FetchesSpecificTag verifies the --tag flag fetches the exact
// tag endpoint and renders that tag's envelope.
func TestGet_Tag_FetchesSpecificTag(t *testing.T) {
	v1 := agentTagFixture("acme-bot", "1")
	v2 := agentTagFixture("acme-bot", "2")
	srv, captured := tagGetServer(t, v2, v1)
	deps := setupClientForServer(t, srv)

	out := &bytes.Buffer{}
	cmd := commands.NewGetCmd(deps)
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"agent", "acme-bot", "--tag", "1", "-o", "json"})
	require.NoError(t, cmd.Execute())

	var got v1alpha1.Agent
	require.NoError(t, json.Unmarshal(out.Bytes(), &got))
	assert.Equal(t, "1", got.Metadata.Tag, "expected tag 1 envelope")
	assert.Equal(t, "v1", got.Spec.Description, "expected v1's spec description")

	// At least one served call should be the exact-tag path.
	require.NotEmpty(t, *captured)
	hitSpecific := false
	for _, p := range *captured {
		// "GET /v0/agents/acme-bot/1" → 3 slashes after "/v0/agents/".
		if p == "GET /v0/agents/acme-bot/1" {
			hitSpecific = true
		}
	}
	assert.True(t, hitSpecific, "expected GET to exact-tag path, got %v", *captured)
}

func TestGet_Tag_FetchesSpecificTagByNamespaceName(t *testing.T) {
	v1 := agentTagFixture("acme-bot", "1")
	v2 := agentTagFixture("acme-bot", "2")
	srv, captured := tagGetServer(t, v2, v1)
	deps := setupClientForServer(t, srv)

	out := &bytes.Buffer{}
	cmd := commands.NewGetCmd(deps)
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"agent", "team-a/acme-bot", "--tag", "1", "-o", "json"})
	require.NoError(t, cmd.Execute())

	require.Contains(t, *captured, "GET /v0/agents/acme-bot/1?namespace=team-a")
}

// TestGet_Tag_DefaultsToLatest verifies that omitting --tag still
// hits the latest endpoint (no regression from --tag wiring).
func TestGet_Tag_DefaultsToLatest(t *testing.T) {
	v1 := agentTagFixture("acme-bot", "1")
	v2 := agentTagFixture("acme-bot", "2")
	srv, captured := tagGetServer(t, v2, v1)
	deps := setupClientForServer(t, srv)

	out := &bytes.Buffer{}
	cmd := commands.NewGetCmd(deps)
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"agent", "acme-bot", "-o", "json"})
	require.NoError(t, cmd.Execute())

	var got v1alpha1.Agent
	require.NoError(t, json.Unmarshal(out.Bytes(), &got))
	assert.Equal(t, "2", got.Metadata.Tag, "expected latest tag 2 envelope")

	// All served calls should be the latest path (no tag segment).
	for _, p := range *captured {
		assert.Equal(t, "GET /v0/agents/acme-bot", p,
			"expected only latest-path GETs, got %v", *captured)
	}
}

// TestGet_Tag_MutuallyExclusiveWithAllTags pins the flag-validation
// guard on runGet.
func TestGet_Tag_MutuallyExclusiveWithAllTags(t *testing.T) {
	cmd := commands.NewGetCmd(declarativeTestDeps(nil))
	cmd.SetArgs([]string{"agent", "acme-bot", "--tag", "1", "--all-tags"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

// TestGet_Tag_NotSupportedForProvider pins that --tag is rejected
// for mutable namespace/name kinds (Runtime, Deployment) before any client
// dispatch happens.
func TestGet_Tag_NotSupportedForProvider(t *testing.T) {
	cmd := commands.NewGetCmd(declarativeTestDeps(nil))
	cmd.SetArgs([]string{"runtime", "my-kagent", "--tag", "1"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--tag not supported")
	assert.Contains(t, err.Error(), "runtime")
}

// TestGet_Tag_NotSupportedForDeployment is the symmetric assertion
// for Deployment.
func TestGet_Tag_NotSupportedForDeployment(t *testing.T) {
	cmd := commands.NewGetCmd(declarativeTestDeps(nil))
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
	deps := setupClientForServer(t, srv)

	cmd := commands.NewGetCmd(deps)
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
	deps := setupClientForServer(t, srv)

	cmd := commands.NewGetCmd(deps)
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
	deps := setupClientForServer(t, srv)

	cmd := commands.NewGetCmd(deps)
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
	cmd := commands.NewGetCmd(declarativeTestDeps(nil))
	cmd.SetArgs([]string{"agents", "--tag", "1", "--latest"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

// TestGet_Latest_NotSupportedForProvider mirrors the --tag guard: --latest
// is also a tag-shaped filter and should be rejected for mutable kinds
// before any dispatch.
func TestGet_Latest_NotSupportedForProvider(t *testing.T) {
	cmd := commands.NewGetCmd(declarativeTestDeps(nil))
	cmd.SetArgs([]string{"runtime", "--latest"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--latest not supported")
	assert.Contains(t, err.Error(), "runtime")
}

// TestGet_Tag_RejectsGetAll pins that --tag is rejected for
// `arctl get all` (cross-kind list flow).
func TestGet_Tag_RejectsGetAll(t *testing.T) {
	cmd := commands.NewGetCmd(declarativeTestDeps(nil))
	cmd.SetArgs([]string{"all", "--tag", "1"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--tag cannot be used with `get all`")
}

// TestGet_Latest_RejectsGetAll is the symmetric guard for --latest on
// cross-kind list.
func TestGet_Latest_RejectsGetAll(t *testing.T) {
	cmd := commands.NewGetCmd(declarativeTestDeps(nil))
	cmd.SetArgs([]string{"all", "--latest"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--latest cannot be used with `get all`")
}
