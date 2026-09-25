package bundle

import (
	"reflect"
	"testing"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

var claudeInventoryCases = []struct {
	name     string
	manifest string
	files    map[string]string
	want     v1alpha1.PluginInventory
}{
	{
		name:     "skills key adds directories to skills/",
		manifest: `{"name":"p","skills":["./extra/"]}`,
		files:    map[string]string{"extra/a/SKILL.md": "x", "skills/b/SKILL.md": "x"},
		want:     v1alpha1.PluginInventory{Skills: []v1alpha1.PluginSkill{{Name: "a"}, {Name: "b"}}},
	},
	{
		name:     "skills directory that holds SKILL.md is one skill",
		manifest: `{"name":"p","skills":"./one/"}`,
		files:    map[string]string{"one/SKILL.md": "x", "one/inner/SKILL.md": "x"},
		want:     v1alpha1.PluginInventory{Skills: []v1alpha1.PluginSkill{{Name: "one"}}},
	},
	{
		name:     "skills key names the plugin root",
		manifest: `{"name":"p","skills":["."]}`,
		files:    map[string]string{"SKILL.md": "---\nname: root\n---\n", "skills/b/SKILL.md": "x"},
		want:     v1alpha1.PluginInventory{Skills: []v1alpha1.PluginSkill{{Name: "root"}, {Name: "b"}}},
	},
	{
		name:     "root SKILL.md alone is one skill",
		manifest: `{"name":"p"}`,
		files:    map[string]string{"SKILL.md": "---\nname: root\n---\n"},
		want:     v1alpha1.PluginInventory{Skills: []v1alpha1.PluginSkill{{Name: "root"}}},
	},
	{
		name:     "root SKILL.md is skipped when the skills key is set",
		manifest: `{"name":"p","skills":"./extra/"}`,
		files:    map[string]string{"SKILL.md": "x"},
	},
	{
		name:     "commands paths replace commands/",
		manifest: `{"name":"p","commands":["./extra-cmds/","./one.md"]}`,
		files:    map[string]string{"commands/old.md": "x", "extra-cmds/a.md": "x", "one.md": "x", "two.md": "x"},
		want:     v1alpha1.PluginInventory{Commands: []string{"a", "one"}},
	},
	{
		name:     "commands map replaces commands/",
		manifest: `{"name":"p","commands":{"status":{"source":"./commands/status.md"},"about":{"content":"x"}}}`,
		files:    map[string]string{"commands/old.md": "x", "commands/status.md": "x"},
		want:     v1alpha1.PluginInventory{Commands: []string{"about", "status"}},
	},
	{
		name:     "agents paths replace agents/",
		manifest: `{"name":"p","agents":["./custom/reviewer.md"]}`,
		files:    map[string]string{"agents/old.md": "x", "custom/reviewer.md": "x", "custom/other.md": "x"},
		want:     v1alpha1.PluginInventory{Agents: []string{"reviewer"}},
	},
	{
		name:     "mcpServers files, inline maps, and bundles merge with .mcp.json",
		manifest: `{"name":"p","mcpServers":["./mcp/servers.json",{"inline":{"command":"x"}},"./tool.mcpb"]}`,
		files: map[string]string{
			".mcp.json":        `{"dot":{"command":"x"}}`,
			"mcp/servers.json": `{"mcpServers":{"file":{"command":"x"}}}`,
		},
		want: v1alpha1.PluginInventory{MCPServers: []string{"./tool.mcpb", "dot", "file", "inline"}},
	},
	{
		name:     "inline mcpServers that reuse a name list it once",
		manifest: `{"name":"p","mcpServers":{"db":{"command":"y"}}}`,
		files:    map[string]string{".mcp.json": `{"mcpServers":{"db":{"command":"x"},"api":{"url":"x"}}}`},
		want:     v1alpha1.PluginInventory{MCPServers: []string{"api", "db"}},
	},
	{
		name:     "hooks files and inline maps merge with hooks/hooks.json",
		manifest: `{"name":"p","hooks":["./hk/extra.json",{"Stop":[{"hooks":[{"type":"prompt"}]}]}]}`,
		files: map[string]string{
			"hooks/hooks.json": `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command"}]}]}}`,
			"hk/extra.json":    `{"hooks":{"PreToolUse":[{"hooks":[{"type":"http"},{"type":"command"}]}]}}`,
		},
		want: v1alpha1.PluginInventory{Hooks: []v1alpha1.PluginHook{
			{Event: "PreToolUse", Type: "command"}, {Event: "PreToolUse", Type: "http"}, {Event: "Stop", Type: "prompt"},
		}},
	},
	{
		name:     "inline hooks object",
		manifest: `{"name":"p","hooks":{"Stop":[{"hooks":[{"type":"command"}]}]}}`,
		want:     v1alpha1.PluginInventory{Hooks: []v1alpha1.PluginHook{{Event: "Stop", Type: "command"}}},
	},
	{
		name:     "hooks file without the hooks wrapper is ignored",
		manifest: `{"name":"p","hooks":"./hk/flat.json"}`,
		files:    map[string]string{"hk/flat.json": `{"Stop":[{"hooks":[{"type":"command"}]}]}`},
	},
	{
		name:     "paths that leave the plugin root are ignored",
		manifest: `{"name":"p","commands":"./../x.md","agents":["/abs.md"],"hooks":"../h.json","mcpServers":"./../m.json"}`,
		files:    map[string]string{"commands/old.md": "x", "agents/old.md": "x"},
	},
}

func TestClaudeInventoryReadsManifestComponents(t *testing.T) {
	for _, tt := range claudeInventoryCases {
		t.Run(tt.name, func(t *testing.T) {
			b := &CanonicalBundle{Files: map[string][]byte{ManifestPath: []byte(tt.manifest)}}
			for p, content := range tt.files {
				b.Files[p] = []byte(content)
			}
			if got := BuildInventory(b, v1alpha1.PluginFormatClaudePlugin); !reflect.DeepEqual(*got, tt.want) {
				t.Fatalf("inventory = %+v, want %+v", *got, tt.want)
			}
		})
	}
}
