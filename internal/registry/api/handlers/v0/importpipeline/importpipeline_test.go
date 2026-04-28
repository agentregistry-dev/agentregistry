//go:build integration

package importpipeline_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/importpipeline"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/importer"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/resource"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

// stubScanner is a deterministic Scanner used to assert that
// POST /v0/import actually drives the enrichment pipeline. Production
// scanners live under pkg/importer/scanners/{osv,scorecard}/.
type stubScanner struct {
	name    string
	calls   int
	annos   map[string]string
	finding *importer.Finding
}

func (s *stubScanner) Name() string                      { return s.name }
func (s *stubScanner) Supports(obj v1alpha1.Object) bool { return true }
func (s *stubScanner) Scan(ctx context.Context, obj v1alpha1.Object) (importer.ScanResult, error) {
	s.calls++
	res := importer.ScanResult{Annotations: s.annos}
	if s.finding != nil {
		res.Findings = []importer.Finding{*s.finding}
	}
	return res, nil
}

func newImportTestServer(t *testing.T, scanners ...importer.Scanner) (*v1alpha1store.Store, *importer.FindingsStore, humatest.TestAPI) {
	t.Helper()
	pool := v1alpha1store.NewTestPool(t)
	agents := v1alpha1store.NewStore(pool, "v1alpha1.agents")
	stores := map[string]*v1alpha1store.Store{
		v1alpha1.KindAgent: agents,
	}
	findings := importer.NewFindingsStore(pool)
	imp, err := importer.New(importer.Config{
		Stores:   stores,
		Findings: findings,
		Scanners: scanners,
	})
	require.NoError(t, err)

	_, api := humatest.New(t)
	importpipeline.Register(api, importpipeline.Config{
		BasePrefix: "/v0",
		Importer:   imp,
	})
	return agents, findings, api
}

const importAgentYAML = `apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  namespace: default
  name: alice
  version: v1
spec:
  title: Alice
`

func TestRegisterImport_Create(t *testing.T) {
	agents, _, api := newImportTestServer(t)

	resp := api.Post("/v0/import",
		"Content-Type: application/yaml",
		strings.NewReader(importAgentYAML))
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	var out struct {
		Results []struct {
			Status     string `json:"status"`
			Kind       string `json:"kind"`
			Name       string `json:"name"`
			Generation int64  `json:"generation"`
		} `json:"results"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &out))
	require.Len(t, out.Results, 1)
	require.Equal(t, "created", out.Results[0].Status)
	require.Equal(t, "Agent", out.Results[0].Kind)
	require.EqualValues(t, 1, out.Results[0].Generation)

	obj, err := agents.Get(context.Background(), "default", "alice", "v1")
	require.NoError(t, err)
	require.Equal(t, "alice", obj.Metadata.Name)
}

func TestRegisterImport_EnrichInvokesScanners(t *testing.T) {
	sc := &stubScanner{
		name:  "fake",
		annos: map[string]string{"security.agentregistry.solo.io/fake-status": "clean"},
		finding: &importer.Finding{
			Severity: "low",
			ID:       "FAKE-1",
			Data:     map[string]any{"note": "hi"},
		},
	}
	agents, findings, api := newImportTestServer(t, sc)

	resp := api.Post("/v0/import?enrich=true",
		"Content-Type: application/yaml",
		strings.NewReader(importAgentYAML))
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	require.Equal(t, 1, sc.calls)

	// Annotation landed on the row.
	obj, err := agents.Get(context.Background(), "default", "alice", "v1")
	require.NoError(t, err)
	require.Equal(t, "clean", obj.Metadata.Annotations["security.agentregistry.solo.io/fake-status"])

	// Finding landed in the side table.
	fs, err := findings.List(context.Background(), v1alpha1.KindAgent, "default", "alice", "v1")
	require.NoError(t, err)
	require.Len(t, fs, 1)
	require.Equal(t, "FAKE-1", fs[0].ID)
}

func TestRegisterImport_EnrichFalseSkipsScanners(t *testing.T) {
	sc := &stubScanner{name: "fake"}
	_, _, api := newImportTestServer(t, sc)

	resp := api.Post("/v0/import",
		"Content-Type: application/yaml",
		strings.NewReader(importAgentYAML))
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	require.Equal(t, 0, sc.calls)
}

func TestRegisterImport_WhichScansFilters(t *testing.T) {
	a := &stubScanner{name: "osv"}
	b := &stubScanner{name: "scorecard"}
	_, _, api := newImportTestServer(t, a, b)

	resp := api.Post("/v0/import?enrich=true&scans=osv",
		"Content-Type: application/yaml",
		strings.NewReader(importAgentYAML))
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	require.Equal(t, 1, a.calls)
	require.Equal(t, 0, b.calls)
}

func TestRegisterImport_DryRunDoesNotWrite(t *testing.T) {
	agents, _, api := newImportTestServer(t)

	resp := api.Post("/v0/import?dryRun=true",
		"Content-Type: application/yaml",
		strings.NewReader(importAgentYAML))
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	var out struct {
		Results []struct {
			Status string `json:"status"`
		} `json:"results"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &out))
	require.Equal(t, "dry-run", out.Results[0].Status)

	_, err := agents.Get(context.Background(), "default", "alice", "v1")
	require.Error(t, err) // not found
}

func TestRegisterImport_InvalidYAMLSurfacesAsFailedResult(t *testing.T) {
	_, _, api := newImportTestServer(t)

	resp := api.Post("/v0/import",
		"Content-Type: application/yaml",
		strings.NewReader("not-valid-yaml: : :"))
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	var out struct {
		Results []struct {
			Status string `json:"status"`
			Error  string `json:"error"`
		} `json:"results"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &out))
	require.Len(t, out.Results, 1)
	require.Equal(t, "failed", out.Results[0].Status)
	require.Contains(t, out.Results[0].Error, "decode")
}

// TestRegisterImport_PerDocAuthorize pins the per-kind RBAC invariant
// for POST /v0/import: each decoded document fires Authorize before
// Upsert. Without this, the import endpoint would be a write-bypass
// for any kind the Importer accepts (denied users could create or
// replace rows by routing writes through this endpoint).
//
// Multi-doc batch: deny "secret" Agent → that doc fails with
// Status=failed; "ok" Agent succeeds. Mirrors the per-doc-failure
// pattern in pkg/registry/resource/apply.go.
func TestRegisterImport_PerDocAuthorize(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	agents := v1alpha1store.NewStore(pool, "v1alpha1.agents")
	stores := map[string]*v1alpha1store.Store{v1alpha1.KindAgent: agents}

	imp, err := importer.New(importer.Config{Stores: stores})
	require.NoError(t, err)

	authorizers := map[string]func(ctx context.Context, in resource.AuthorizeInput) error{
		v1alpha1.KindAgent: func(ctx context.Context, in resource.AuthorizeInput) error {
			if in.Name == "secret" {
				return huma.Error403Forbidden("denied")
			}
			return nil
		},
	}

	_, api := humatest.New(t)
	importpipeline.Register(api, importpipeline.Config{
		BasePrefix:  "/v0",
		Importer:    imp,
		Authorizers: authorizers,
	})

	yaml := `apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  namespace: default
  name: secret
  version: v1
spec:
  title: Secret
---
apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  namespace: default
  name: ok
  version: v1
spec:
  title: Ok
`
	resp := api.Post("/v0/import",
		"Content-Type: application/yaml",
		strings.NewReader(yaml))
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	var out struct {
		Results []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
			Error  string `json:"error"`
		} `json:"results"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &out))
	require.Len(t, out.Results, 2)

	// Denied doc → failed; error mentions authorize so operators can
	// distinguish from validation failures.
	require.Equal(t, "secret", out.Results[0].Name)
	require.Equal(t, "failed", out.Results[0].Status)
	require.Contains(t, out.Results[0].Error, "authorize")

	// Allowed doc → created.
	require.Equal(t, "ok", out.Results[1].Name)
	require.Equal(t, "created", out.Results[1].Status)

	// Denied row was NOT persisted.
	_, err = agents.Get(context.Background(), "default", "secret", "v1")
	require.Error(t, err)
	// Allowed row IS persisted.
	row, err := agents.Get(context.Background(), "default", "ok", "v1")
	require.NoError(t, err)
	require.Equal(t, "ok", row.Metadata.Name)
}

// TestRegisterImport_DeniesKindWithNoAuthorizer pins the
// defense-in-depth fail-closed: when Authorizers is non-empty (the
// caller intends to gate writes), a decoded doc whose Kind has no
// entry in the map must DENY rather than silently allow. The
// enterprise H2 boot guard already ensures every OSS BuiltinKinds
// entry has an authorizer; this catches downstream kinds an operator
// adds without updating the import config.
//
// Configures the importer with two kinds (Agent + MCPServer) but
// only an Agent authorizer. POST an MCPServer doc → fail-closed.
func TestRegisterImport_DeniesKindWithNoAuthorizer(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	stores := map[string]*v1alpha1store.Store{
		v1alpha1.KindAgent:     v1alpha1store.NewStore(pool, "v1alpha1.agents"),
		v1alpha1.KindMCPServer: v1alpha1store.NewStore(pool, "v1alpha1.mcpservers"),
	}
	imp, err := importer.New(importer.Config{Stores: stores})
	require.NoError(t, err)

	// Note: no MCPServer entry — that's the defense-in-depth scenario.
	authorizers := map[string]func(ctx context.Context, in resource.AuthorizeInput) error{
		v1alpha1.KindAgent: func(ctx context.Context, in resource.AuthorizeInput) error { return nil },
	}

	_, api := humatest.New(t)
	importpipeline.Register(api, importpipeline.Config{
		BasePrefix:  "/v0",
		Importer:    imp,
		Authorizers: authorizers,
	})

	yaml := `apiVersion: ar.dev/v1alpha1
kind: MCPServer
metadata:
  namespace: default
  name: anything
  version: v1
spec:
  title: Anything
`
	resp := api.Post("/v0/import",
		"Content-Type: application/yaml",
		strings.NewReader(yaml))
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	var out struct {
		Results []struct {
			Status string `json:"status"`
			Error  string `json:"error"`
		} `json:"results"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &out))
	require.Len(t, out.Results, 1)
	require.Equal(t, "failed", out.Results[0].Status)
	require.Contains(t, out.Results[0].Error, "no authorizer wired for kind",
		"missing-authorizer must fail closed when Authorizers is non-empty")

	// Row is NOT persisted.
	_, err = stores[v1alpha1.KindMCPServer].Get(context.Background(), "default", "anything", "v1")
	require.Error(t, err)
}

func TestRegisterImport_MultiDocPerDocResults(t *testing.T) {
	_, _, api := newImportTestServer(t)

	yaml := importAgentYAML + `---
apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  namespace: default
  name: bob
  version: v1
spec:
  title: Bob
`
	resp := api.Post("/v0/import",
		"Content-Type: application/yaml",
		strings.NewReader(yaml))
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	var out struct {
		Results []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"results"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &out))
	require.Len(t, out.Results, 2)
	require.Equal(t, "alice", out.Results[0].Name)
	require.Equal(t, "bob", out.Results[1].Name)
}
