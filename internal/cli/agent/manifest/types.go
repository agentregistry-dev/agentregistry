// Package manifest hosts the runtime projection of a v1alpha1.Agent
// envelope used by `arctl agent run`. The on-disk shape is always the
// envelope (decoded by project.LoadAgent); this package builds the
// in-memory pairing of the envelope with the resolved per-MCP-server
// runtime data via Resolve, then hands that pairing to the runtime
// (run.go, project.go templates) for compose / mcp_tools render.
//
// The package never serializes anything, never reads or writes
// agent.yaml, and never duplicates fields that already exist on
// v1alpha1.Agent. Resolved skills + prompts are NOT projected here:
// callers iterate agent.Spec.Skills / agent.Spec.Prompts directly,
// since those refs are passed straight through to the materialize-time
// fetch helpers (resolveSkillsForRuntime, ResolveManifestPrompts).
package manifest

import (
	"path"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// ResolvedAgent pairs a v1alpha1.Agent envelope with the resolved
// runtime form of its MCPServer refs. The envelope is the source of
// truth for every other field — name, image, model, skill / prompt
// refs — and is held by pointer (no copying).
//
// The runtime never mutates Agent; mutations to MCPServers are
// permitted (run.go's stage step rewrites it for buildable subsets).
type ResolvedAgent struct {
	Agent      *v1alpha1.Agent
	MCPServers []ResolvedMCPServer
}

// ResolvedMCPServer is one terminal-form MCP server entry on the
// runtime manifest. Resolve always populates entries with Type="command"
// or Type="remote"; no intermediate "registry" state is ever exposed.
//
//   - Type="command": runnable container (Image/Build/Command/Args/Env
//     populated). Build is "registry/<name>" for npm/PyPI packages that
//     must be built into a Docker image at run time; empty for OCI images
//     that are pulled directly.
//   - Type="remote":  remote MCP endpoint (URL/Headers populated).
//
// Fields are mutually exclusive by Type. This struct is never user-serialized.
type ResolvedMCPServer struct {
	Name    string
	Version string

	Type    string
	Image   string
	Build   string
	Command string
	Args    []string
	Env     []string
	URL     string
	Headers map[string]string
}

// RefBasename returns the path-basename of a v1alpha1.ResourceRef's Name,
// i.e. "fetch" for "acme/fetch". The runtime uses this when projecting
// a registry ref onto a local filesystem-safe identifier (skill
// directory, MCP server compose service name, prompts.json key).
//
// Lives next to ResolvedMCPServer because every site that needs a
// basename — including the local representation of MCPServers — passes
// through this helper, keeping the basename rule in one place.
func RefBasename(refName string) string {
	if refName == "" {
		return ""
	}
	return path.Base(refName)
}
