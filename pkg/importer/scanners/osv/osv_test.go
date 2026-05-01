//go:build unit

package osv

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/importer"
)

// fakeGitHub serves a configurable map of repo-path → file bytes.
// Every request path looks like /repos/{owner}/{repo}/contents/{path};
// unknown paths return 404 so tests exercise the "missing manifest is
// non-fatal" behavior.
type fakeGitHub struct{ files map[string][]byte }

func newFakeGitHub(files map[string][]byte) *httptest.Server {
	return httptest.NewServer(&fakeGitHub{files: files})
}

func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if len(parts) < 5 {
		http.NotFound(w, r)
		return
	}
	path := strings.Join(parts[4:], "/")
	data, ok := f.files[path]
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{
		"content":  base64.StdEncoding.EncodeToString(data),
		"encoding": "base64",
	})
}

// fakeOSV echoes preloaded vuln payloads keyed by
// (ecosystem|name|version). Any query not in the map returns no
// vulns (clean). The server writes an OSV batch response shape
// matching the real API so osv.go's existing decoder handles it.
type fakeOSV struct{ vulns map[string][]osvFakeVuln }

type osvFakeVuln struct {
	ID       string            `json:"id"`
	Severity []osvFakeSeverity `json:"severity,omitempty"`
}

type osvFakeSeverity struct {
	Type  string `json:"type"`
	Score string `json:"score"`
}

func newFakeOSV(vulns map[string][]osvFakeVuln) *httptest.Server {
	return httptest.NewServer(&fakeOSV{vulns: vulns})
}

func (f *fakeOSV) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Queries []struct {
			Package struct {
				Name      string `json:"name"`
				Ecosystem string `json:"ecosystem"`
			} `json:"package"`
			Version string `json:"version"`
		} `json:"queries"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	type respEntry struct {
		Vulns []osvFakeVuln `json:"vulns"`
	}
	results := make([]respEntry, len(req.Queries))
	for i, q := range req.Queries {
		key := q.Package.Ecosystem + "|" + q.Package.Name + "|" + q.Version
		results[i].Vulns = f.vulns[key]
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"results": results})
}

func newScanner(t *testing.T, gh, osv *httptest.Server) *Scanner {
	t.Helper()
	return New(Config{
		HTTPClient:             http.DefaultClient,
		OSVEndpoint:            osv.URL,
		GitHubContentsEndpoint: gh.URL + "/repos/{owner}/{repo}/contents/{path}",
	})
}

// -----------------------------------------------------------------------------
// Supports
// -----------------------------------------------------------------------------

func TestSupports_MCPServerWithGitHubRepo(t *testing.T) {
	obj := &v1alpha1.MCPServer{
		Spec: v1alpha1.MCPServerSpec{
			Repository: &v1alpha1.Repository{URL: "https://github.com/org/repo"},
		},
	}
	require.True(t, New(Config{}).Supports(obj))
}

func TestSupports_AgentWithSSHURL(t *testing.T) {
	obj := &v1alpha1.Agent{
		Spec: v1alpha1.AgentSpec{
			Source: &v1alpha1.AgentSource{
				Repository: &v1alpha1.Repository{URL: "git@github.com:org/repo.git"},
			},
		},
	}
	require.True(t, New(Config{}).Supports(obj))
}

func TestSupports_SkillWithNoRepo(t *testing.T) {
	require.False(t, New(Config{}).Supports(&v1alpha1.Skill{}))
}

func TestSupports_PromptUnsupported(t *testing.T) {
	require.False(t, New(Config{}).Supports(&v1alpha1.Prompt{}))
}

func TestSupports_NonGitHubRejected(t *testing.T) {
	obj := &v1alpha1.MCPServer{
		Spec: v1alpha1.MCPServerSpec{
			Repository: &v1alpha1.Repository{URL: "https://gitlab.com/org/repo"},
		},
	}
	require.False(t, New(Config{}).Supports(obj))
}

// -----------------------------------------------------------------------------
// Scan
// -----------------------------------------------------------------------------

func goModFixture() []byte {
	return []byte(`module example.com/foo
go 1.22
require (
	github.com/bar/baz v1.2.3
	github.com/qux/wibble v0.1.0
)
`)
}

func TestScan_CleanWhenNoManifestsFound(t *testing.T) {
	gh := newFakeGitHub(nil)
	osv := newFakeOSV(nil)
	t.Cleanup(func() { gh.Close(); osv.Close() })

	obj := &v1alpha1.MCPServer{
		Spec: v1alpha1.MCPServerSpec{
			Repository: &v1alpha1.Repository{URL: "https://github.com/org/repo"},
		},
	}
	res, err := newScanner(t, gh, osv).Scan(context.Background(), obj)
	require.NoError(t, err)
	require.Equal(t, "clean", res.Annotations[importer.AnnoOSVStatus])
	require.Empty(t, res.Findings)
}

func TestScan_CleanWhenNoVulns(t *testing.T) {
	gh := newFakeGitHub(map[string][]byte{"go.mod": goModFixture()})
	osv := newFakeOSV(nil)
	t.Cleanup(func() { gh.Close(); osv.Close() })

	obj := &v1alpha1.MCPServer{
		Spec: v1alpha1.MCPServerSpec{
			Repository: &v1alpha1.Repository{URL: "https://github.com/org/repo"},
		},
	}
	res, err := newScanner(t, gh, osv).Scan(context.Background(), obj)
	require.NoError(t, err)
	require.Equal(t, "clean", res.Annotations[importer.AnnoOSVStatus])
	require.Equal(t, "0", res.Annotations[importer.AnnoOSVCountCritical])
}

func TestScan_VulnerableEmitsFindingsAndCounts(t *testing.T) {
	gh := newFakeGitHub(map[string][]byte{"go.mod": goModFixture()})
	osv := newFakeOSV(map[string][]osvFakeVuln{
		"Go|github.com/bar/baz|v1.2.3": {
			{ID: "GHSA-CRITICAL-1", Severity: []osvFakeSeverity{{Type: "CVSS_V3", Score: "9.5"}}},
			{ID: "GHSA-HIGH-1", Severity: []osvFakeSeverity{{Type: "CVSS_V3", Score: "7.5"}}},
		},
		"Go|github.com/qux/wibble|v0.1.0": {
			{ID: "GHSA-MEDIUM-1", Severity: []osvFakeSeverity{{Type: "CVSS_V3", Score: "5.0"}}},
		},
	})
	t.Cleanup(func() { gh.Close(); osv.Close() })

	obj := &v1alpha1.MCPServer{
		Spec: v1alpha1.MCPServerSpec{
			Repository: &v1alpha1.Repository{URL: "https://github.com/org/repo"},
		},
	}
	res, err := newScanner(t, gh, osv).Scan(context.Background(), obj)
	require.NoError(t, err)

	require.Equal(t, "vulnerable", res.Annotations[importer.AnnoOSVStatus])
	require.Equal(t, "vulnerable", res.Labels[importer.AnnoOSVStatus])
	require.Equal(t, "1", res.Annotations[importer.AnnoOSVCountCritical])
	require.Equal(t, "1", res.Annotations[importer.AnnoOSVCountHigh])
	require.Equal(t, "1", res.Annotations[importer.AnnoOSVCountMedium])

	require.Len(t, res.Findings, 3)
	severityOf := map[string]string{}
	for _, f := range res.Findings {
		severityOf[f.ID] = f.Severity
	}
	require.Equal(t, "critical", severityOf["GHSA-CRITICAL-1"])
	require.Equal(t, "high", severityOf["GHSA-HIGH-1"])
	require.Equal(t, "medium", severityOf["GHSA-MEDIUM-1"])
}

func TestScan_MultiEcosystemManifests(t *testing.T) {
	gh := newFakeGitHub(map[string][]byte{
		"package-lock.json": []byte(`{"packages":{"node_modules/left-pad":{"version":"1.0.0","name":"left-pad"}}}`),
		"requirements.txt":  []byte("requests==2.31.0\n"),
		"go.mod":            goModFixture(),
	})
	osv := newFakeOSV(nil)
	t.Cleanup(func() { gh.Close(); osv.Close() })

	obj := &v1alpha1.MCPServer{
		Spec: v1alpha1.MCPServerSpec{
			Repository: &v1alpha1.Repository{URL: "https://github.com/org/repo"},
		},
	}
	res, err := newScanner(t, gh, osv).Scan(context.Background(), obj)
	require.NoError(t, err)
	require.Equal(t, "clean", res.Annotations[importer.AnnoOSVStatus])
}

func TestScan_OSVErrorSurfaces(t *testing.T) {
	gh := newFakeGitHub(map[string][]byte{"go.mod": goModFixture()})
	osv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusServiceUnavailable)
	}))
	t.Cleanup(func() { gh.Close(); osv.Close() })

	obj := &v1alpha1.MCPServer{
		Spec: v1alpha1.MCPServerSpec{
			Repository: &v1alpha1.Repository{URL: "https://github.com/org/repo"},
		},
	}
	_, err := newScanner(t, gh, osv).Scan(context.Background(), obj)
	require.Error(t, err)
	require.Contains(t, err.Error(), "osv")
}

func TestScan_GitHubFetchErrorIsNonFatal(t *testing.T) {
	// 500 on every contents request — scanner treats as missing
	// manifests and returns clean.
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	osv := newFakeOSV(nil)
	t.Cleanup(func() { bad.Close(); osv.Close() })

	obj := &v1alpha1.MCPServer{
		Spec: v1alpha1.MCPServerSpec{
			Repository: &v1alpha1.Repository{URL: "https://github.com/org/repo"},
		},
	}
	res, err := newScanner(t, bad, osv).Scan(context.Background(), obj)
	require.NoError(t, err)
	require.Equal(t, "clean", res.Annotations[importer.AnnoOSVStatus])
}

// -----------------------------------------------------------------------------
// Parsers (ported verbatim from legacy osv_scan.go) — regression guards
// -----------------------------------------------------------------------------

func TestParseGoModForOSV_SingleLine(t *testing.T) {
	out := parseGoModForOSV([]byte("require github.com/foo/bar v1.2.3\n"))
	require.Len(t, out, 1)
	require.Equal(t, "github.com/foo/bar", out[0].Package.Name)
	require.Equal(t, "v1.2.3", out[0].Version)
	require.Equal(t, "Go", out[0].Package.Ecosystem)
}

func TestParseGoModForOSV_Block(t *testing.T) {
	out := parseGoModForOSV([]byte(`require (
	github.com/a/b v1.0.0
	github.com/c/d v2.3.4
)
`))
	require.Len(t, out, 2)
	require.ElementsMatch(t, []string{"github.com/a/b", "github.com/c/d"},
		[]string{out[0].Package.Name, out[1].Package.Name})
}

func TestParsePipRequirementsForOSV_SkipsComments(t *testing.T) {
	out := parsePipRequirementsForOSV([]byte("# comment\nflask==2.0.1\n\nrequests>=2.0\n"))
	require.Len(t, out, 1)
	require.Equal(t, "flask", out[0].Package.Name)
	require.Equal(t, "2.0.1", out[0].Version)
}

func TestParseNPMLockForOSV_DerivesNameFromPath(t *testing.T) {
	out := parseNPMLockForOSV([]byte(`{"packages":{"node_modules/foo":{"version":"1.0.0"}}}`))
	require.Len(t, out, 1)
	require.Equal(t, "foo", out[0].Package.Name)
}

// -----------------------------------------------------------------------------
// Misc
// -----------------------------------------------------------------------------

func TestName(t *testing.T) {
	require.Equal(t, "osv", New(Config{}).Name())
}
