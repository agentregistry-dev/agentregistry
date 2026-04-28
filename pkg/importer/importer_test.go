//go:build integration

package importer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

const (
	testNS        = "default"
	agentsTable   = "v1alpha1.agents"
	mcpTable      = "v1alpha1.mcp_servers"
	skillsTable   = "v1alpha1.skills"
	promptsTable  = "v1alpha1.prompts"
	provTable     = "v1alpha1.providers"
	deployTable   = "v1alpha1.deployments"
	testScanner   = "test-scanner"
	failedScanner = "failing-scanner"
)

func newTestImporter(t *testing.T, extra ...Scanner) (*Importer, *v1alpha1store.Store, *FindingsStore) {
	t.Helper()
	pool := v1alpha1store.NewTestPool(t)

	agents := v1alpha1store.NewStore(pool, agentsTable)
	stores := map[string]*v1alpha1store.Store{
		v1alpha1.KindAgent:      agents,
		v1alpha1.KindMCPServer:  v1alpha1store.NewStore(pool, mcpTable),
		v1alpha1.KindSkill:      v1alpha1store.NewStore(pool, skillsTable),
		v1alpha1.KindPrompt:     v1alpha1store.NewStore(pool, promptsTable),
		v1alpha1.KindProvider:   v1alpha1store.NewStore(pool, provTable),
		v1alpha1.KindDeployment: v1alpha1store.NewStore(pool, deployTable),
	}
	findings := NewFindingsStore(pool)
	imp, err := New(Config{
		Stores:   stores,
		Findings: findings,
		Scanners: extra,
	})
	require.NoError(t, err)
	return imp, agents, findings
}

func writeYAML(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	return p
}

const agentYAML = `
apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  namespace: default
  name: demo
  version: v1.0.0
spec:
  title: Demo Agent
  description: A test agent
`

func TestImport_CreatesAgent(t *testing.T) {
	imp, agents, _ := newTestImporter(t)
	dir := t.TempDir()
	writeYAML(t, dir, "agent.yaml", agentYAML)

	results, err := imp.Import(context.Background(), Options{Path: dir})
	require.NoError(t, err)
	require.Len(t, results, 1)

	r := results[0]
	require.Equal(t, ImportStatusCreated, r.Status, "error=%s", r.Error)
	require.EqualValues(t, 1, r.Generation)
	require.Equal(t, EnrichmentStatusSkipped, r.EnrichmentStatus)

	obj, err := agents.Get(context.Background(), testNS, "demo", "v1.0.0")
	require.NoError(t, err)
	require.Equal(t, "demo", obj.Metadata.Name)
}

func TestImport_ReimportUnchanged(t *testing.T) {
	imp, _, _ := newTestImporter(t)
	dir := t.TempDir()
	writeYAML(t, dir, "agent.yaml", agentYAML)

	_, err := imp.Import(context.Background(), Options{Path: dir})
	require.NoError(t, err)
	results, err := imp.Import(context.Background(), Options{Path: dir})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, ImportStatusUnchanged, results[0].Status)
	require.EqualValues(t, 1, results[0].Generation)
}

func TestImport_InvalidValidationFails(t *testing.T) {
	imp, _, _ := newTestImporter(t)
	dir := t.TempDir()
	// Missing required metadata.name.
	writeYAML(t, dir, "bad.yaml", `
apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  namespace: default
  version: v1
spec:
  title: X
`)
	results, err := imp.Import(context.Background(), Options{Path: dir})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, ImportStatusFailed, results[0].Status)
	require.Contains(t, results[0].Error, "validation")
}

func TestImport_UnknownKindFails(t *testing.T) {
	imp, _, _ := newTestImporter(t)
	dir := t.TempDir()
	writeYAML(t, dir, "bad.yaml", `
apiVersion: ar.dev/v1alpha1
kind: NotAKind
metadata:
  namespace: default
  name: x
  version: v1
spec: {}
`)
	results, err := imp.Import(context.Background(), Options{Path: dir})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, ImportStatusFailed, results[0].Status)
	// Scheme rejects unknown kinds at decode time.
	require.Contains(t, results[0].Error, "decode")
}

func TestImport_DryRunDoesNotWrite(t *testing.T) {
	imp, agents, _ := newTestImporter(t)
	dir := t.TempDir()
	writeYAML(t, dir, "agent.yaml", agentYAML)

	results, err := imp.Import(context.Background(), Options{Path: dir, DryRun: true})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, ImportStatusDryRun, results[0].Status)

	_, err = agents.Get(context.Background(), testNS, "demo", "v1.0.0")
	require.Error(t, err) // not found
}

func TestImport_NamespaceDefaultApplied(t *testing.T) {
	imp, agents, _ := newTestImporter(t)
	dir := t.TempDir()
	writeYAML(t, dir, "agent.yaml", `
apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  name: noNS
  version: v1
spec:
  title: no-ns
`)
	results, err := imp.Import(context.Background(), Options{Path: dir, Namespace: "team-a"})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, ImportStatusCreated, results[0].Status, "error=%s", results[0].Error)
	require.Equal(t, "team-a", results[0].Namespace)

	obj, err := agents.Get(context.Background(), "team-a", "noNS", "v1")
	require.NoError(t, err)
	require.Equal(t, "team-a", obj.Metadata.Namespace)
}

// -----------------------------------------------------------------------------
// Scanner integration
// -----------------------------------------------------------------------------

// fakeScanner produces deterministic output so we can assert merges
// end-to-end without network dependencies.
type fakeScanner struct {
	name      string
	supports  bool
	returnErr error
	annos     map[string]string
	labels    map[string]string
	findings  []Finding
	callCount int
}

func (f *fakeScanner) Name() string                      { return f.name }
func (f *fakeScanner) Supports(obj v1alpha1.Object) bool { return f.supports }
func (f *fakeScanner) Scan(ctx context.Context, obj v1alpha1.Object) (ScanResult, error) {
	f.callCount++
	if f.returnErr != nil {
		return ScanResult{}, f.returnErr
	}
	return ScanResult{
		Annotations: f.annos,
		Labels:      f.labels,
		Findings:    f.findings,
	}, nil
}

func TestImport_EnrichMergesAnnotationsAndFindings(t *testing.T) {
	scanner := &fakeScanner{
		name:     testScanner,
		supports: true,
		annos: map[string]string{
			AnnoOSVStatus: "clean",
		},
		labels: map[string]string{
			AnnoOSVStatus: "clean",
		},
		findings: []Finding{
			{Severity: "low", ID: "CVE-2024-0001", Data: map[string]any{"note": "x"}},
		},
	}
	imp, agents, findings := newTestImporter(t, scanner)
	dir := t.TempDir()
	writeYAML(t, dir, "agent.yaml", agentYAML)

	results, err := imp.Import(context.Background(), Options{Path: dir, Enrich: true})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, ImportStatusCreated, results[0].Status, "error=%s", results[0].Error)
	require.Equal(t, EnrichmentStatusOK, results[0].EnrichmentStatus)
	require.Empty(t, results[0].EnrichmentErrors)
	require.Equal(t, 1, scanner.callCount)

	// Row carries merged annotations + labels + last-scanned-at.
	obj, err := agents.Get(context.Background(), testNS, "demo", "v1.0.0")
	require.NoError(t, err)
	require.Equal(t, "clean", obj.Metadata.Annotations[AnnoOSVStatus])
	require.Equal(t, "clean", obj.Metadata.Labels[AnnoOSVStatus])
	require.NotEmpty(t, obj.Metadata.Annotations[AnnoLastScannedAt])
	require.Equal(t, defaultScannedBy, obj.Metadata.Annotations[AnnoLastScannedBy])

	// Findings row landed.
	fs, err := findings.List(context.Background(), v1alpha1.KindAgent, testNS, "demo", "v1.0.0")
	require.NoError(t, err)
	require.Len(t, fs, 1)
	require.Equal(t, "CVE-2024-0001", fs[0].ID)
}

func TestImport_EnrichReplacesFindingsOnRescan(t *testing.T) {
	scanner := &fakeScanner{
		name:     testScanner,
		supports: true,
		findings: []Finding{
			{Severity: "high", ID: "CVE-2024-OLD", Data: map[string]any{}},
		},
	}
	imp, _, findings := newTestImporter(t, scanner)
	dir := t.TempDir()
	writeYAML(t, dir, "agent.yaml", agentYAML)

	_, err := imp.Import(context.Background(), Options{Path: dir, Enrich: true})
	require.NoError(t, err)

	// Second scan returns a different finding set — old one should go.
	scanner.findings = []Finding{
		{Severity: "low", ID: "CVE-2024-NEW", Data: map[string]any{}},
	}
	_, err = imp.Import(context.Background(), Options{Path: dir, Enrich: true})
	require.NoError(t, err)

	fs, err := findings.List(context.Background(), v1alpha1.KindAgent, testNS, "demo", "v1.0.0")
	require.NoError(t, err)
	require.Len(t, fs, 1)
	require.Equal(t, "CVE-2024-NEW", fs[0].ID)
}

func TestImport_EnrichPartialOnScannerError(t *testing.T) {
	ok := &fakeScanner{
		name:     testScanner,
		supports: true,
		annos:    map[string]string{AnnoOSVStatus: "clean"},
	}
	bad := &fakeScanner{
		name:      failedScanner,
		supports:  true,
		returnErr: errors.New("boom"),
	}
	imp, _, _ := newTestImporter(t, ok, bad)
	dir := t.TempDir()
	writeYAML(t, dir, "agent.yaml", agentYAML)

	results, err := imp.Import(context.Background(), Options{Path: dir, Enrich: true})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, EnrichmentStatusPartial, results[0].EnrichmentStatus)
	require.Len(t, results[0].EnrichmentErrors, 1)
	require.Contains(t, results[0].EnrichmentErrors[0], failedScanner)
}

func TestImport_EnrichFailedWhenAllScannersError(t *testing.T) {
	bad := &fakeScanner{
		name:      failedScanner,
		supports:  true,
		returnErr: errors.New("boom"),
	}
	imp, _, _ := newTestImporter(t, bad)
	dir := t.TempDir()
	writeYAML(t, dir, "agent.yaml", agentYAML)

	results, err := imp.Import(context.Background(), Options{Path: dir, Enrich: true})
	require.NoError(t, err)
	require.Equal(t, EnrichmentStatusFailed, results[0].EnrichmentStatus)
	// Upsert still succeeded per design.
	require.Equal(t, ImportStatusCreated, results[0].Status, "error=%s", results[0].Error)
}

func TestImport_EnrichSkippedWhenNoSupportingScanner(t *testing.T) {
	scanner := &fakeScanner{name: testScanner, supports: false}
	imp, _, _ := newTestImporter(t, scanner)
	dir := t.TempDir()
	writeYAML(t, dir, "agent.yaml", agentYAML)

	results, err := imp.Import(context.Background(), Options{Path: dir, Enrich: true})
	require.NoError(t, err)
	require.Equal(t, EnrichmentStatusSkipped, results[0].EnrichmentStatus)
	require.Equal(t, 0, scanner.callCount)
}

func TestImport_WhichScansFilters(t *testing.T) {
	a := &fakeScanner{name: "osv", supports: true}
	b := &fakeScanner{name: "scorecard", supports: true}
	imp, _, _ := newTestImporter(t, a, b)
	dir := t.TempDir()
	writeYAML(t, dir, "agent.yaml", agentYAML)

	_, err := imp.Import(context.Background(), Options{
		Path: dir, Enrich: true, WhichScans: []string{"osv"},
	})
	require.NoError(t, err)
	require.Equal(t, 1, a.callCount)
	require.Equal(t, 0, b.callCount)
}

func TestImport_MultiDocYAML(t *testing.T) {
	imp, _, _ := newTestImporter(t)
	dir := t.TempDir()
	writeYAML(t, dir, "multi.yaml", agentYAML+`
---
apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  namespace: default
  name: demo2
  version: v1
spec:
  title: Demo 2
`)
	results, err := imp.Import(context.Background(), Options{Path: dir})
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Equal(t, ImportStatusCreated, results[0].Status, "error=%s", results[0].Error)
	require.Equal(t, ImportStatusCreated, results[1].Status, "error=%s", results[1].Error)
}
