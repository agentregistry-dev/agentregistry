package commands_test

import (
	"bytes"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	arv0 "github.com/agentregistry-dev/agentregistry/pkg/api/v0"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/cli"
)

// namespaceRequest is one request seen by namespaceTestServer.
type namespaceRequest struct {
	Method    string
	Path      string
	Namespace string
	HasNS     bool
}

// namespaceTestServer serves the registry endpoints the namespace tests touch
// and records every request's method, path, and ?namespace= value.
type namespaceTestServer struct {
	mu       sync.Mutex
	requests []namespaceRequest
	// listItems is returned by every list endpoint.
	listItems []any
	srv       *httptest.Server
}

func newNamespaceTestServer(t *testing.T) *namespaceTestServer {
	t.Helper()
	s := &namespaceTestServer{}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *namespaceTestServer) serve(w http.ResponseWriter, r *http.Request) {
	ns, hasNS := r.URL.Query()["namespace"]
	req := namespaceRequest{Method: r.Method, Path: r.URL.Path, HasNS: hasNS}
	if hasNS {
		req.Namespace = ns[0]
	}
	s.mu.Lock()
	s.requests = append(s.requests, req)
	s.mu.Unlock()

	namespace := v1alpha1.DefaultNamespace
	if req.Namespace != "" {
		namespace = req.Namespace
	}
	w.Header().Set("Content-Type", "application/json")
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	switch {
	case r.URL.Path == "/v0/apply":
		writeNamespaceJSON(w, arv0.ApplyResultsResponse{Results: []arv0.ApplyResult{{
			Kind: v1alpha1.KindAgent, Namespace: namespace, Name: "acme-bot", Status: arv0.ApplyStatusCreated,
		}}})
	case r.Method == http.MethodDelete:
		w.WriteHeader(http.StatusNoContent)
	case len(parts) == 2:
		writeNamespaceJSON(w, map[string]any{"items": s.listItems})
	case len(parts) == 4 && parts[3] == "tags":
		writeNamespaceJSON(w, map[string]any{"items": []any{namespaceAgent(namespace, parts[2])}})
	case parts[1] == "deployments":
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"detail":"deployment not found"}`))
	case parts[1] == "skills":
		writeNamespaceJSON(w, v1alpha1.Skill{
			TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindSkill},
			Metadata: v1alpha1.ObjectMeta{Namespace: namespace, Name: parts[2], Tag: "latest"},
		})
	default:
		writeNamespaceJSON(w, namespaceAgent(namespace, parts[2]))
	}
}

func (s *namespaceTestServer) recorded() []namespaceRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]namespaceRequest(nil), s.requests...)
}

func writeNamespaceJSON(w http.ResponseWriter, v any) {
	_ = json.NewEncoder(w).Encode(v)
}

func namespaceAgent(namespace, name string) v1alpha1.Agent {
	return v1alpha1.Agent{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindAgent},
		Metadata: v1alpha1.ObjectMeta{Namespace: namespace, Name: name, Tag: "latest"},
	}
}

// runArctl executes the full arctl root against srv with the given env, so
// the persistent --namespace flag and ARCTL_NAMESPACE are wired as in the
// real binary.
func runArctl(t *testing.T, srv *namespaceTestServer, env map[string]string, args ...string) (string, error) {
	t.Helper()
	e := declarativeTestEnv{"ARCTL_API_BASE_URL": srv.srv.URL}
	maps.Copy(e, env)
	root := cli.Root(cli.Config{Env: e})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func TestNamespaceSelection_Requests(t *testing.T) {
	file := writeTempYAML(t, agentYAML)
	tests := []struct {
		name string
		env  map[string]string
		args []string
		want []namespaceRequest
	}{
		{
			name: "list default namespace",
			args: []string{"get", "agents"},
			want: []namespaceRequest{{Method: http.MethodGet, Path: "/v0/agents", Namespace: "default", HasNS: true}},
		},
		{
			name: "list with -n",
			args: []string{"get", "agents", "-n", "team-a"},
			want: []namespaceRequest{{Method: http.MethodGet, Path: "/v0/agents", Namespace: "team-a", HasNS: true}},
		},
		{
			name: "list with root --namespace before the verb",
			args: []string{"--namespace", "team-a", "get", "agents"},
			want: []namespaceRequest{{Method: http.MethodGet, Path: "/v0/agents", Namespace: "team-a", HasNS: true}},
		},
		{
			name: "list with ARCTL_NAMESPACE",
			env:  map[string]string{"ARCTL_NAMESPACE": "team-a"},
			args: []string{"get", "agents"},
			want: []namespaceRequest{{Method: http.MethodGet, Path: "/v0/agents", Namespace: "team-a", HasNS: true}},
		},
		{
			name: "flag overrides ARCTL_NAMESPACE",
			env:  map[string]string{"ARCTL_NAMESPACE": "team-a"},
			args: []string{"get", "agents", "-n", "team-b"},
			want: []namespaceRequest{{Method: http.MethodGet, Path: "/v0/agents", Namespace: "team-b", HasNS: true}},
		},
		{
			name: "list every namespace",
			args: []string{"get", "agents", "-A"},
			want: []namespaceRequest{{Method: http.MethodGet, Path: "/v0/agents", Namespace: "all", HasNS: true}},
		},
		{
			name: "-A overrides a selected namespace",
			env:  map[string]string{"ARCTL_NAMESPACE": "team-a"},
			args: []string{"get", "deployments", "--all-namespaces"},
			want: []namespaceRequest{{Method: http.MethodGet, Path: "/v0/deployments", Namespace: "all", HasNS: true}},
		},
		{
			name: "get NAME in the default namespace omits the query",
			args: []string{"get", "agent", "acme-bot"},
			want: []namespaceRequest{{Method: http.MethodGet, Path: "/v0/agents/acme-bot"}},
		},
		{
			name: "get NAME with -n",
			args: []string{"get", "agent", "acme-bot", "-n", "team-a"},
			want: []namespaceRequest{{Method: http.MethodGet, Path: "/v0/agents/acme-bot", Namespace: "team-a", HasNS: true}},
		},
		{
			name: "get NAMESPACE/NAME without a selection",
			args: []string{"get", "agent", "team-a/acme-bot"},
			want: []namespaceRequest{{Method: http.MethodGet, Path: "/v0/agents/acme-bot", Namespace: "team-a", HasNS: true}},
		},
		{
			name: "get NAMESPACE/NAME matching -n",
			args: []string{"get", "agent", "team-a/acme-bot", "-n", "team-a"},
			want: []namespaceRequest{{Method: http.MethodGet, Path: "/v0/agents/acme-bot", Namespace: "team-a", HasNS: true}},
		},
		{
			name: "get --all-tags with -n",
			args: []string{"get", "agent", "acme-bot", "--all-tags", "-n", "team-a"},
			want: []namespaceRequest{{Method: http.MethodGet, Path: "/v0/agents/acme-bot/tags", Namespace: "team-a", HasNS: true}},
		},
		{
			name: "delete NAME with -n",
			args: []string{"delete", "agent", "acme-bot", "-n", "team-a"},
			want: []namespaceRequest{
				{Method: http.MethodGet, Path: "/v0/agents/acme-bot", Namespace: "team-a", HasNS: true},
				{Method: http.MethodDelete, Path: "/v0/agents/acme-bot/latest", Namespace: "team-a", HasNS: true},
			},
		},
		{
			name: "delete --all-tags with NAMESPACE/NAME",
			args: []string{"delete", "agent", "team-a/acme-bot", "--all-tags"},
			want: []namespaceRequest{
				{Method: http.MethodGet, Path: "/v0/agents/acme-bot/tags", Namespace: "team-a", HasNS: true},
				{Method: http.MethodDelete, Path: "/v0/agents/acme-bot/latest", Namespace: "team-a", HasNS: true},
			},
		},
		{
			name: "apply without a selection keeps document namespaces",
			args: []string{"apply", "-f", file},
			want: []namespaceRequest{{Method: http.MethodPost, Path: "/v0/apply"}},
		},
		{
			name: "apply with -n",
			args: []string{"apply", "-f", file, "-n", "team-a"},
			want: []namespaceRequest{{Method: http.MethodPost, Path: "/v0/apply", Namespace: "team-a", HasNS: true}},
		},
		{
			name: "apply with ARCTL_NAMESPACE",
			env:  map[string]string{"ARCTL_NAMESPACE": "team-a"},
			args: []string{"apply", "-f", file},
			want: []namespaceRequest{{Method: http.MethodPost, Path: "/v0/apply", Namespace: "team-a", HasNS: true}},
		},
		{
			name: "delete -f with -n",
			args: []string{"delete", "-f", file, "-n", "team-a"},
			want: []namespaceRequest{{Method: http.MethodDelete, Path: "/v0/apply", Namespace: "team-a", HasNS: true}},
		},
		{
			name: "wait with -n",
			args: []string{"wait", "deployment", "summarizer", "--for=delete", "--timeout=0", "-n", "team-a"},
			want: []namespaceRequest{{Method: http.MethodGet, Path: "/v0/deployments/summarizer", Namespace: "team-a", HasNS: true}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newNamespaceTestServer(t)
			out, err := runArctl(t, srv, tt.env, tt.args...)
			require.NoError(t, err, out)
			assert.Equal(t, tt.want, srv.recorded())
		})
	}
}

func TestNamespaceSelection_GetAllUsesSelectedNamespace(t *testing.T) {
	srv := newNamespaceTestServer(t)
	out, err := runArctl(t, srv, nil, "get", "all", "-n", "team-a")
	require.NoError(t, err, out)
	assert.Contains(t, out, `No resources found in namespace "team-a".`)

	requests := srv.recorded()
	require.NotEmpty(t, requests)
	for _, req := range requests {
		assert.Equal(t, "team-a", req.Namespace, req.Path)
	}
}

func TestNamespaceSelection_Rejections(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		args []string
		want string
	}{
		{
			name: "NAMESPACE/NAME conflicting with -n",
			args: []string{"get", "agent", "team-b/acme-bot", "-n", "team-a"},
			want: `namespace "team-b" in "team-b/acme-bot" conflicts with selected namespace "team-a"`,
		},
		{
			name: "NAMESPACE/NAME conflicting with ARCTL_NAMESPACE",
			env:  map[string]string{"ARCTL_NAMESPACE": "team-a"},
			args: []string{"delete", "agent", "team-b/acme-bot"},
			want: `conflicts with selected namespace "team-a"`,
		},
		{
			name: "all as a NAMESPACE/NAME namespace",
			args: []string{"get", "agent", "all/acme-bot"},
			want: `"all" in "all/acme-bot" is not a namespace`,
		},
		{
			name: "all as the selected namespace",
			args: []string{"get", "agents", "-n", "all"},
			want: "use -A/--all-namespaces",
		},
		{
			name: "all from ARCTL_NAMESPACE on a write",
			env:  map[string]string{"ARCTL_NAMESPACE": "all"},
			args: []string{"delete", "agent", "acme-bot"},
			want: "use -A/--all-namespaces",
		},
		{
			name: "-A with a NAME",
			args: []string{"get", "agent", "acme-bot", "-A"},
			want: "cannot be combined with a resource NAME",
		},
		{
			name: "conflicting wait reference",
			args: []string{"wait", "deployment", "team-b/summarizer", "-n", "team-a"},
			want: "conflicts with selected namespace",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newNamespaceTestServer(t)
			out, err := runArctl(t, srv, tt.env, tt.args...)
			require.ErrorContains(t, err, tt.want, out)
			assert.Empty(t, srv.recorded(), "rejected commands must not reach the registry")
		})
	}
}

func TestNamespaceSelection_AllNamespacesTable(t *testing.T) {
	srv := newNamespaceTestServer(t)
	srv.listItems = []any{namespaceAgent("default", "alpha"), namespaceAgent("team-a", "beta")}

	out, err := runArctl(t, srv, nil, "get", "agents", "-A")
	require.NoError(t, err, out)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	require.Len(t, lines, 3, out)
	assert.Regexp(t, `^NAMESPACE\s+NAME\s+TAG`, lines[0])
	assert.Regexp(t, `^default\s+alpha\s+latest`, lines[1])
	assert.Regexp(t, `^team-a\s+beta\s+latest`, lines[2])

	out, err = runArctl(t, srv, nil, "get", "agents", "-n", "team-a")
	require.NoError(t, err, out)
	assert.NotContains(t, out, "NAMESPACE", "a single-namespace list keeps the kind's own columns")
}

func TestNamespaceSelection_Output(t *testing.T) {
	file := writeTempYAML(t, agentYAML)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "empty list names the namespace", args: []string{"get", "agents", "-n", "team-a"}, want: `No agents found in namespace "team-a".`},
		{name: "empty default list is unchanged", args: []string{"get", "agents"}, want: "No agents found.\n"},
		{name: "apply result names the namespace", args: []string{"apply", "-f", file, "-n", "team-a"}, want: "Agent/team-a/acme-bot created"},
		{name: "default apply result is unchanged", args: []string{"apply", "-f", file}, want: "Agent/acme-bot created"},
		{name: "delete names the namespace", args: []string{"delete", "agent", "acme-bot", "-n", "team-a"}, want: "Deleted: agent/team-a/acme-bot"},
		{name: "wait names the namespace", args: []string{"wait", "deployment", "summarizer", "--for=delete", "--timeout=0", "-n", "team-a"}, want: "deployment/team-a/summarizer deleted"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newNamespaceTestServer(t)
			out, err := runArctl(t, srv, nil, tt.args...)
			require.NoError(t, err, out)
			assert.Contains(t, out, tt.want)
		})
	}
}

func TestNamespaceSelection_PullUsesSelectedNamespace(t *testing.T) {
	srv := newNamespaceTestServer(t)
	_, err := runArctl(t, srv, nil, "pull", "skill", "summarize", t.TempDir(), "-n", "team-a")
	// The fixture skill has no source repository, so pull stops after the
	// lookup; the lookup is what this test pins.
	require.ErrorContains(t, err, `skill "team-a/summarize" has no source repository URL set`)
	assert.Equal(t, []namespaceRequest{{Method: http.MethodGet, Path: "/v0/skills/summarize", Namespace: "team-a", HasNS: true}}, srv.recorded())
}
