package controller

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/registry/plugins/bundle"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

// fakeComponents is a per-kind componentGetter backed by a map keyed
// "ns/name:tag".
type fakeComponents map[string]*v1alpha1.RawObject

func (f fakeComponents) Get(_ context.Context, ns, name, tag string) (*v1alpha1.RawObject, error) {
	if raw, ok := f[ns+"/"+name+":"+tag]; ok {
		return raw, nil
	}
	return nil, pkgdb.ErrNotFound
}

func rawComponent(t *testing.T, kind, ns, name, tag string, spec any, status any) *v1alpha1.RawObject {
	t.Helper()
	specRaw, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	raw := &v1alpha1.RawObject{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: kind},
		Metadata: v1alpha1.ObjectMeta{Namespace: ns, Name: name, Tag: tag, Generation: 1},
		Spec:     specRaw,
	}
	if status != nil {
		if raw.Status, err = json.Marshal(status); err != nil {
			t.Fatal(err)
		}
	}
	return raw
}

func componentsFixture(t *testing.T, skillStatus any) map[string]componentGetter {
	t.Helper()
	skills := fakeComponents{
		"default/log-triage:v3": rawComponent(t, v1alpha1.KindSkill, "default", "log-triage", "v3",
			v1alpha1.SkillSpec{Source: &v1alpha1.SkillSource{Repository: &v1alpha1.Repository{URL: "https://github.com/o/skill"}}},
			skillStatus),
	}
	mcps := fakeComponents{
		"default/pagerduty:latest": rawComponent(t, v1alpha1.KindMCPServer, "default", "pagerduty", "latest",
			v1alpha1.MCPServerSpec{Remote: &v1alpha1.MCPRemote{Type: "http", URL: "https://mcp.example.com"}}, nil),
		"default/oci-only:latest": rawComponent(t, v1alpha1.KindMCPServer, "default", "oci-only", "latest",
			v1alpha1.MCPServerSpec{Source: &v1alpha1.MCPServerSource{Package: &v1alpha1.MCPPackage{
				Origin: v1alpha1.MCPPackageOrigin{Type: v1alpha1.MCPPackageOriginTypeOCI, Identifier: "ghcr.io/x/y@sha256:abc", OCI: &v1alpha1.MCPPackageOriginOCI{ServerName: "y"}},
			}}}, nil),
	}
	prompts := fakeComponents{
		"default/declare-incident:latest": rawComponent(t, v1alpha1.KindPrompt, "default", "declare-incident", "latest",
			v1alpha1.PromptSpec{Content: "# declare"}, nil),
		"default/guidelines:latest": rawComponent(t, v1alpha1.KindPrompt, "default", "guidelines", "latest",
			v1alpha1.PromptSpec{Content: "follow the runbook"}, nil),
	}
	return map[string]componentGetter{
		v1alpha1.KindSkill:     skills,
		v1alpha1.KindMCPServer: mcps,
		v1alpha1.KindPrompt:    prompts,
	}
}

func fakeTree(files map[string][]byte) treeFetcher {
	return func(context.Context, *v1alpha1.Repository, string) (*bundle.CanonicalBundle, error) {
		return &bundle.CanonicalBundle{Files: files}, nil
	}
}

func composedPlugin(gen int64) *v1alpha1.Plugin {
	return &v1alpha1.Plugin{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "ir", Tag: "v1", Generation: gen},
		Spec: v1alpha1.PluginSpec{
			Title:        "Incident Response",
			Skills:       []v1alpha1.ComponentRef{{Name: "log-triage", Tag: "v3"}},
			MCPServers:   []v1alpha1.ComponentRef{{Name: "pagerduty"}},
			Commands:     []v1alpha1.ComponentRef{{Name: "declare-incident"}},
			Instructions: &v1alpha1.ComponentRef{Name: "guidelines"},
		},
	}
}

func TestPluginReconcile_PureComposition(t *testing.T) {
	store := newFakePluginStore()
	skillStatus := map[string]any{"resolvedSource": map[string]string{"commit": strings.Repeat("a", 40)}}
	c := &PluginController{
		Store:      store,
		Resolver:   fakeResolver{}, // must not be consulted: no base source
		Components: componentsFixture(t, skillStatus),
		fetchTree:  fakeTree(map[string][]byte{"SKILL.md": []byte("---\nname: log-triage\n---\ntriage")}),
	}
	outcome, _, err := c.reconcile(context.Background(), composedPlugin(2))
	if err != nil || outcome != "resolved" {
		t.Fatalf("reconcile = (%q, %v), want (resolved, nil)", outcome, err)
	}
	got := store.plugin(t, "default", "ir", "v1")
	if !got.Status.IsConditionTrue(pluginReadyCondition) {
		t.Fatal("expected Ready=True")
	}
	if got.Status.ResolvedSource != nil {
		t.Errorf("pure composition must have nil resolvedSource, got %+v", got.Status.ResolvedSource)
	}
	pins := got.Status.ResolvedComponents
	if len(pins) != 4 {
		t.Fatalf("expected 4 pins, got %+v", pins)
	}
	// Spec order: skills, mcpServers, commands, instructions.
	if pins[0].Kind != v1alpha1.KindSkill || pins[0].Tag != "v3" || pins[0].Commit != strings.Repeat("a", 40) {
		t.Errorf("skill pin = %+v", pins[0])
	}
	if pins[1].Kind != v1alpha1.KindMCPServer || pins[1].Tag != "latest" || pins[1].ContentHash == "" {
		t.Errorf("mcp pin = %+v", pins[1])
	}
	if pins[2].Kind != v1alpha1.KindPrompt || pins[2].Name != "declare-incident" {
		t.Errorf("command pin = %+v", pins[2])
	}
	if pins[3].Kind != v1alpha1.KindPrompt || pins[3].Name != "guidelines" {
		t.Errorf("instructions pin = %+v", pins[3])
	}
	// Manifest generated (no base) + inventory reflects the composed bundle.
	if got.Status.Manifest == nil || got.Status.Manifest.Name != "ir" {
		t.Errorf("generated manifest = %+v", got.Status.Manifest)
	}
	inv := got.Status.Inventory
	if inv == nil || len(inv.Skills) != 1 || inv.Skills[0].Name != "log-triage" {
		t.Errorf("inventory skills = %+v", inv)
	}
	if len(inv.Commands) != 1 || inv.Commands[0] != "declare-incident" {
		t.Errorf("inventory commands = %+v", inv)
	}
	if len(inv.MCPServers) != 1 || inv.MCPServers[0] != "pagerduty" {
		t.Errorf("inventory mcpServers = %+v", inv)
	}
}

func TestPluginReconcile_SkillPendingIsRetryable(t *testing.T) {
	store := newFakePluginStore()
	c := &PluginController{
		Store:      store,
		Resolver:   fakeResolver{},
		Components: componentsFixture(t, nil), // skill has no resolvedSource yet
		fetchTree:  fakeTree(nil),
	}
	_, _, err := c.reconcile(context.Background(), composedPlugin(2))
	if err == nil || !errors.Is(err, errComponentsPending) {
		t.Fatalf("expected retryable ComponentsPending error, got %v", err)
	}
	got := store.plugin(t, "default", "ir", "v1")
	if got.Status.ObservedGeneration != 0 {
		t.Errorf("retryable must NOT bump observedGeneration, got %d", got.Status.ObservedGeneration)
	}
	if r := readyReason(got); r != "ComponentsPending" {
		t.Errorf("ready reason = %q, want ComponentsPending", r)
	}
}

func TestPluginReconcile_MissingComponentIsTerminal(t *testing.T) {
	store := newFakePluginStore()
	p := composedPlugin(3)
	p.Spec.Skills = []v1alpha1.ComponentRef{{Name: "does-not-exist"}}
	c := &PluginController{
		Store:      store,
		Resolver:   fakeResolver{},
		Components: componentsFixture(t, nil),
		fetchTree:  fakeTree(nil),
	}
	outcome, reason, err := c.reconcile(context.Background(), p)
	if err != nil {
		t.Fatalf("terminal must Forget, got %v", err)
	}
	if outcome != "failed" || reason != "ComponentMissing" {
		t.Fatalf("got (%q, %q), want (failed, ComponentMissing)", outcome, reason)
	}
	if got := store.plugin(t, "default", "ir", "v1"); got.Status.ObservedGeneration != 3 {
		t.Errorf("terminal must bump observedGeneration, got %d", got.Status.ObservedGeneration)
	}
}

func TestPluginReconcile_OCIPackageMCPIsTerminalInvalid(t *testing.T) {
	store := newFakePluginStore()
	p := composedPlugin(4)
	p.Spec.Skills = nil
	p.Spec.Commands = nil
	p.Spec.Instructions = nil
	p.Spec.MCPServers = []v1alpha1.ComponentRef{{Name: "oci-only"}}
	c := &PluginController{
		Store:      store,
		Resolver:   fakeResolver{},
		Components: componentsFixture(t, nil),
		fetchTree:  fakeTree(nil),
	}
	outcome, reason, err := c.reconcile(context.Background(), p)
	if err != nil {
		t.Fatalf("terminal must Forget, got %v", err)
	}
	if outcome != "failed" || reason != "ComponentInvalid" {
		t.Fatalf("got (%q, %q), want (failed, ComponentInvalid)", outcome, reason)
	}
}

func TestPluginReconcile_BasePlusOverlayComposes(t *testing.T) {
	store := newFakePluginStore()
	skillStatus := map[string]any{"resolvedSource": map[string]string{"commit": strings.Repeat("b", 40)}}
	p := composedPlugin(5)
	p.Spec.MCPServers, p.Spec.Commands, p.Spec.Instructions = nil, nil, nil
	p.Spec.Source = &v1alpha1.PluginSource{
		Type: v1alpha1.PluginSourceTypeGit,
		Git:  &v1alpha1.PluginSourceGit{Repository: &v1alpha1.Repository{URL: "https://github.com/o/base", Branch: "main"}},
	}
	baseManifest := `{"name":"base-plugin"}`
	c := &PluginController{
		Store: store,
		Resolver: fakeResolver{
			resolved: &v1alpha1.PluginResolvedSource{Type: v1alpha1.PluginSourceTypeGit, Commit: "basecommit"},
			bundle: &bundle.CanonicalBundle{Files: map[string][]byte{
				".claude-plugin/plugin.json": []byte(baseManifest),
				"skills/log-triage/SKILL.md": []byte("---\nname: log-triage\n---\nold"),
			}},
		},
		Components: componentsFixture(t, skillStatus),
		fetchTree:  fakeTree(map[string][]byte{"SKILL.md": []byte("---\nname: log-triage\ndescription: curated\n---\nnew")}),
	}
	outcome, _, err := c.reconcile(context.Background(), p)
	if err != nil || outcome != "resolved" {
		t.Fatalf("reconcile = (%q, %v), want (resolved, nil)", outcome, err)
	}
	got := store.plugin(t, "default", "ir", "v1")
	if got.Status.ResolvedSource == nil || got.Status.ResolvedSource.Commit != "basecommit" {
		t.Errorf("resolvedSource = %+v", got.Status.ResolvedSource)
	}
	// Base manifest passes through untouched.
	if got.Status.Manifest == nil || got.Status.Manifest.Name != "base-plugin" {
		t.Errorf("manifest = %+v", got.Status.Manifest)
	}
	// Inventory reflects the overlay-wins result: the curated skill's
	// description, not the base's.
	inv := got.Status.Inventory
	if inv == nil || len(inv.Skills) != 1 || inv.Skills[0].Description != "curated" {
		t.Errorf("inventory = %+v", inv)
	}
}

// TestPluginReconcile_SkillDestUsesDeclaredName guards the cross-lane naming
// contract (BYO composition doc / Agent Skills spec): the skills/<name>/
// directory is keyed by the SKILL.md-declared name, not the registry ref name.
func TestPluginReconcile_SkillDestUsesDeclaredName(t *testing.T) {
	store := newFakePluginStore()
	skillStatus := map[string]any{"resolvedSource": map[string]string{"commit": strings.Repeat("c", 40)}}
	p := composedPlugin(2)
	p.Spec.MCPServers, p.Spec.Commands, p.Spec.Instructions = nil, nil, nil
	// Ref name "log-triage" but the fetched tree declares "triage-pro".
	c := &PluginController{
		Store:      store,
		Resolver:   fakeResolver{},
		Components: componentsFixture(t, skillStatus),
		fetchTree:  fakeTree(map[string][]byte{"SKILL.md": []byte("---\nname: triage-pro\n---\nbody")}),
	}
	outcome, _, err := c.reconcile(context.Background(), p)
	if err != nil || outcome != "resolved" {
		t.Fatalf("reconcile = (%q, %v), want (resolved, nil)", outcome, err)
	}
	got := store.plugin(t, "default", "ir", "v1")
	inv := got.Status.Inventory
	if inv == nil || len(inv.Skills) != 1 || inv.Skills[0].Name != "triage-pro" {
		t.Errorf("inventory should reflect the declared name, got %+v", inv)
	}
	// The pin keeps the REGISTRY identity (ref name), for re-resolution.
	if len(got.Status.ResolvedComponents) != 1 || got.Status.ResolvedComponents[0].Name != "log-triage" {
		t.Errorf("pin should keep the ref name, got %+v", got.Status.ResolvedComponents)
	}
}

// TestPluginReconcile_InvalidDeclaredSkillNameIsTerminal: a fetched skill tree
// with no/invalid SKILL.md name cannot be placed spec-compliantly.
func TestPluginReconcile_InvalidDeclaredSkillNameIsTerminal(t *testing.T) {
	store := newFakePluginStore()
	skillStatus := map[string]any{"resolvedSource": map[string]string{"commit": strings.Repeat("d", 40)}}
	p := composedPlugin(3)
	p.Spec.MCPServers, p.Spec.Commands, p.Spec.Instructions = nil, nil, nil
	c := &PluginController{
		Store:      store,
		Resolver:   fakeResolver{},
		Components: componentsFixture(t, skillStatus),
		fetchTree:  fakeTree(map[string][]byte{"SKILL.md": []byte("---\nname: Bad--Name\n---\n")}),
	}
	outcome, reason, err := c.reconcile(context.Background(), p)
	if err != nil {
		t.Fatalf("terminal must Forget, got %v", err)
	}
	if outcome != "failed" || reason != "ComponentInvalid" {
		t.Fatalf("got (%q, %q), want (failed, ComponentInvalid)", outcome, reason)
	}
}

// TestPluginReconcile_DeclaredNameCollisionIsTerminal: two refs whose trees
// declare the same SKILL.md name would collide at skills/<name>/.
func TestPluginReconcile_DeclaredNameCollisionIsTerminal(t *testing.T) {
	store := newFakePluginStore()
	commit := strings.Repeat("e", 40)
	skills := fakeComponents{
		"default/skill-a:latest": rawComponent(t, v1alpha1.KindSkill, "default", "skill-a", "latest",
			v1alpha1.SkillSpec{Source: &v1alpha1.SkillSource{Repository: &v1alpha1.Repository{URL: "https://github.com/o/a"}}},
			map[string]any{"resolvedSource": map[string]string{"commit": commit}}),
		"default/skill-b:latest": rawComponent(t, v1alpha1.KindSkill, "default", "skill-b", "latest",
			v1alpha1.SkillSpec{Source: &v1alpha1.SkillSource{Repository: &v1alpha1.Repository{URL: "https://github.com/o/b"}}},
			map[string]any{"resolvedSource": map[string]string{"commit": commit}}),
	}
	p := composedPlugin(4)
	p.Spec.Skills = []v1alpha1.ComponentRef{{Name: "skill-a"}, {Name: "skill-b"}}
	p.Spec.MCPServers, p.Spec.Commands, p.Spec.Instructions = nil, nil, nil
	c := &PluginController{
		Store:      store,
		Resolver:   fakeResolver{},
		Components: map[string]componentGetter{v1alpha1.KindSkill: skills},
		// Both trees declare the same name.
		fetchTree: fakeTree(map[string][]byte{"SKILL.md": []byte("---\nname: same-name\n---\n")}),
	}
	outcome, reason, err := c.reconcile(context.Background(), p)
	if err != nil {
		t.Fatalf("terminal must Forget, got %v", err)
	}
	if outcome != "failed" || reason != "ComponentInvalid" {
		t.Fatalf("got (%q, %q), want (failed, ComponentInvalid)", outcome, reason)
	}
}
