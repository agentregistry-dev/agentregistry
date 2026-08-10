package pluginmarketplace

import (
	"errors"
	"fmt"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

var (
	// ErrNotResolved is returned for a Plugin that isn't Ready with a resolved
	// source pin yet. Callers should skip it, never emit a partial entry.
	ErrNotResolved = errors.New("plugin not yet resolved")
	// ErrUnsupportedSource is returned for a Plugin whose resolved source type
	// has no marketplace.json representation (OCI).
	ErrUnsupportedSource = errors.New("resolved source type has no marketplace.json representation")
	// ErrComposed is returned for a Plugin with composition refs. A composed
	// plugin has no single upstream git URL — serving just its base source
	// would silently drop the overlays — so it is skipped until the registry
	// can serve compiled bundles (git-backed marketplace,
	// agentregistry-enterprise#1195). Consume composed plugins via
	// deploy-time materialization or arctl pull instead.
	ErrComposed = errors.New("composed plugin has no single-source marketplace.json representation")
)

// nameSep joins namespace and name into a marketplace.json-safe qualified
// name. Claude Code's plugin marketplace schema forbids "/" in a plugin
// name, so "." is used instead (matching VS Code's ${publisher}.${name} and
// JetBrains's reverse-DNS plugin IDs).
const nameSep = "."

// qualifiedName always prefixes name with its namespace — including the
// default namespace — so the wire name is always fully qualified. Mirrors
// pkg/mcpregistry.ServerName's rationale for the analogous problem.
func qualifiedName(namespace, name string) string {
	if namespace == "" {
		namespace = v1alpha1.DefaultNamespace
	}
	return namespace + nameSep + name
}

// FromPlugin translates a resolved Plugin into a marketplace.json PluginEntry.
// It returns ErrNotResolved for anything that isn't Ready with a non-nil
// ResolvedSource, or whose Status hasn't caught up with the current Spec
// (ObservedGeneration < Generation) — the reconciler only resets Ready on a
// Plugin's very first reconcile, so a Spec edit to an already-Ready Plugin
// (e.g. a new source URL) leaves Ready true and ResolvedSource pointed at the
// stale commit until the next reconcile finishes. Combining the live Spec URL
// with that stale commit would emit a mismatched, potentially uninstallable
// pin. It returns ErrUnsupportedSource for an OCI-resolved Plugin (the
// marketplace.json schema has no OCI/image source form) — callers must skip
// these, never emit a partial/broken entry.
func FromPlugin(p *v1alpha1.Plugin) (PluginEntry, error) {
	if len(p.Spec.Skills) > 0 || len(p.Spec.MCPServers) > 0 || len(p.Spec.Commands) > 0 || p.Spec.Instructions != nil {
		return PluginEntry{}, ErrComposed
	}
	if !p.Status.IsConditionTrue("Ready") || p.Status.ResolvedSource == nil {
		return PluginEntry{}, ErrNotResolved
	}
	if p.Metadata.Generation > p.Status.ObservedGeneration {
		return PluginEntry{}, ErrNotResolved
	}
	if p.Status.ResolvedSource.Type != v1alpha1.PluginSourceTypeGit {
		return PluginEntry{}, ErrUnsupportedSource
	}
	if p.Spec.Source == nil || p.Spec.Source.Git == nil || p.Spec.Source.Git.Repository == nil {
		return PluginEntry{}, fmt.Errorf("plugin %q: resolved as git but has no git source spec", p.Metadata.Name)
	}

	repo := p.Spec.Source.Git.Repository
	sha := p.Status.ResolvedSource.Commit

	var source any
	if repo.Subfolder != "" {
		source = GitSubdirSource{Source: "git-subdir", URL: repo.URL, Path: repo.Subfolder, SHA: sha}
	} else {
		source = URLSource{Source: "url", URL: repo.URL, SHA: sha}
	}

	entry := PluginEntry{Name: qualifiedName(p.Metadata.NamespaceOrDefault(), p.Metadata.Name), Source: source}
	if p.Status.Manifest != nil {
		entry.Description = p.Status.Manifest.Description
		// Left empty if unset; Claude Code falls back to the resolved SHA.
		entry.Version = p.Status.Manifest.Version
	} else {
		entry.Description = p.Spec.Description
	}
	return entry, nil
}
