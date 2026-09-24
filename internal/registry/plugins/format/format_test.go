package format

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/registry/plugins/bundle"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

const agentPluginsJSON = `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"acme.test"}`

var detectCases = []struct {
	name        string
	files       map[string]string
	wantFormats []v1alpha1.PluginFormat
	wantPath    string
	wantErr     string
}{
	{
		name:        "agent-plugins manifest",
		files:       map[string]string{"plugin.json": agentPluginsJSON, "skills/review/SKILL.md": "x"},
		wantFormats: []v1alpha1.PluginFormat{v1alpha1.PluginFormatAgentPlugins},
		wantPath:    "plugin.json",
	},
	{
		name:        "claude manifest",
		files:       map[string]string{".claude-plugin/plugin.json": `{"name":"code-review","author":{"name":"Anthropic"}}`},
		wantFormats: []v1alpha1.PluginFormat{v1alpha1.PluginFormatClaudePlugin},
		wantPath:    ".claude-plugin/plugin.json",
	},
	{
		name:        "both manifests: root wins",
		files:       map[string]string{"plugin.json": agentPluginsJSON, ".claude-plugin/plugin.json": `{"name":"acme.test"}`},
		wantFormats: []v1alpha1.PluginFormat{v1alpha1.PluginFormatAgentPlugins},
		wantPath:    "plugin.json",
	},
	{
		name:        "unknown keys and non-object extensions are ignored",
		files:       map[string]string{".claude-plugin/plugin.json": `{"name":"a","hooks":5,"interface":{},"extensions":"x"}`},
		wantFormats: []v1alpha1.PluginFormat{v1alpha1.PluginFormatClaudePlugin},
		wantPath:    ".claude-plugin/plugin.json",
	},
	{name: "no manifest", files: map[string]string{"skills/review/SKILL.md": "x"}, wantErr: "no plugin.json or .claude-plugin/plugin.json manifest"},
	{name: "root manifest without schema", files: map[string]string{"plugin.json": `{"name":"acme"}`}, wantErr: `plugin.json: unsupported plugin schema ""`},
	{name: "root manifest with wrong schema", files: map[string]string{"plugin.json": `{"$schema":"https://example.com/s.json","name":"acme"}`}, wantErr: "unsupported plugin schema"},
	{name: "claude manifest without schema is fine but needs a name", files: map[string]string{".claude-plugin/plugin.json": `{}`}, wantErr: `.claude-plugin/plugin.json: invalid plugin name ""`},
	{name: "uppercase name", files: map[string]string{".claude-plugin/plugin.json": `{"name":"Acme"}`}, wantErr: "invalid plugin name"},
	{name: "double dash name", files: map[string]string{".claude-plugin/plugin.json": `{"name":"a--b"}`}, wantErr: "invalid plugin name"},
	{name: "double dot name", files: map[string]string{"plugin.json": `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"a..b"}`}, wantErr: "invalid plugin name"},
	{name: "trailing dash name", files: map[string]string{".claude-plugin/plugin.json": `{"name":"a-"}`}, wantErr: "invalid plugin name"},
	{name: "name over 64 characters", files: map[string]string{".claude-plugin/plugin.json": `{"name":"` + strings.Repeat("a", 65) + `"}`}, wantErr: "invalid plugin name"},
	{name: "string author", files: map[string]string{".claude-plugin/plugin.json": `{"name":"a","author":"x"}`}, wantErr: "cannot unmarshal string"},
	{name: "unknown author key", files: map[string]string{".claude-plugin/plugin.json": `{"name":"a","author":{"name":"x","team":"y"}}`}, wantErr: "unknown field"},
	{name: "keywords not strings", files: map[string]string{".claude-plugin/plugin.json": `{"name":"a","keywords":[1]}`}, wantErr: "cannot unmarshal number"},
	{name: "plugin.json directory", files: map[string]string{"plugin.json/x.json": "{}", ".claude-plugin/plugin.json": `{"name":"a"}`}, wantErr: "plugin.json is a directory"},
	{name: "malformed JSON", files: map[string]string{".claude-plugin/plugin.json": `{bad`}, wantErr: "invalid character"},
}

func TestDetect(t *testing.T) {
	for _, tt := range detectCases {
		t.Run(tt.name, func(t *testing.T) {
			formats, path, err := Detect(bundleOf(tt.files))
			if tt.wantErr != "" {
				if !errors.Is(err, bundle.ErrInvalidBundle) || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want ErrInvalidBundle containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Detect: %v", err)
			}
			if !reflect.DeepEqual(formats, tt.wantFormats) || path != tt.wantPath {
				t.Fatalf("Detect = (%v, %q), want (%v, %q)", formats, path, tt.wantFormats, tt.wantPath)
			}
		})
	}
}

func bundleOf(files map[string]string) *bundle.CanonicalBundle {
	b := &bundle.CanonicalBundle{Files: map[string][]byte{}}
	for p, content := range files {
		b.Files[p] = []byte(content)
	}
	return b
}
