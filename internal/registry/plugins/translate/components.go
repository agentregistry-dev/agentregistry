package translate

import "strings"

// ComponentKind classifies a canonical bundle path. It mirrors the path
// recognition in store.ParseManifest exactly (including the bare-"bin/"
// exclusion) and adds KindAgentsMd and KindManifest, so the two indexes never
// disagree. Unrecognized paths are KindOther (default-pass / supporting files).
type ComponentKind string

const (
	KindSkill    ComponentKind = "skill"
	KindAgent    ComponentKind = "agent"
	KindCommand  ComponentKind = "command"
	KindHooks    ComponentKind = "hooks"
	KindMCP      ComponentKind = "mcp"
	KindBin      ComponentKind = "bin"
	KindAgentsMd ComponentKind = "agents-md"
	KindManifest ComponentKind = "manifest"
	KindOther    ComponentKind = "other"
)

// Classify returns the component kind for a canonical path.
func Classify(p string) ComponentKind {
	switch {
	case p == "SKILL.md" || (strings.HasPrefix(p, "skills/") && strings.HasSuffix(p, "/SKILL.md")):
		return KindSkill
	case p == "AGENTS.md":
		return KindAgentsMd
	case strings.HasPrefix(p, "agents/") && strings.HasSuffix(p, ".md"):
		return KindAgent
	case strings.HasPrefix(p, "commands/") && strings.HasSuffix(p, ".md"):
		return KindCommand
	case strings.HasPrefix(p, "bin/") && p != "bin/":
		return KindBin
	case p == ".mcp.json":
		return KindMCP
	case p == "hooks/hooks.json":
		return KindHooks
	default:
		return KindOther
	}
}
