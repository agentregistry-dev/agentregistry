package translate

func init() { Register(claudeCodeAdapter{}) }

// claudeCodeAdapter maps the canonical bundle to/from the Claude Code plugin
// layout. The canonical form IS Claude-Code-shaped, so translation is lossless
// identity for every file — including the real .claude-plugin/plugin.json
// manifest, which passes through unchanged rather than being regenerated.
// Supporting files, AGENTS.md, and Claude-only extras (themes/, output-styles/)
// all default-pass.
type claudeCodeAdapter struct{}

func (claudeCodeAdapter) Harness() Harness { return HarnessClaudeCode }

func (claudeCodeAdapter) MapToHarness(string) PathMapping   { return PathMapping{} } // identity / default-pass
func (claudeCodeAdapter) MapFromHarness(string) PathMapping { return PathMapping{} } // identity / default-pass
