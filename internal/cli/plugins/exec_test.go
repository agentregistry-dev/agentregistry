package plugins

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderArgs_SubstitutesPerArg(t *testing.T) {
	args := []string{"docker", "build", "-t", "{{.Image}}", "{{.ProjectDir}}"}
	out, err := RenderArgs(args, map[string]string{
		"Image":      "myagent:dev",
		"ProjectDir": "/path/to/proj",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"docker", "build", "-t", "myagent:dev", "/path/to/proj"}, out)
}

func TestRenderArgs_MissingValueErrors(t *testing.T) {
	_, err := RenderArgs([]string{"{{.Missing}}"}, map[string]string{})
	require.Error(t, err)
}

func TestExec_Smoke(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only smoke test")
	}
	cmd := Command{Command: []string{"echo", "{{.Greeting}}"}}
	out, err := ExecCapture(cmd, "/tmp", map[string]string{"Greeting": "hello"})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(out, "hello"))
}

func TestRenderTemplates_CopiesAndSubstitutes(t *testing.T) {
	pluginDir := t.TempDir()
	tplDir := filepath.Join(pluginDir, "templates")
	require.NoError(t, os.MkdirAll(tplDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tplDir, "agent.py.tmpl"), []byte(`name = "{{.Name}}"`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tplDir, "static.txt"), []byte(`no template here`), 0644))

	dst := t.TempDir()
	p := &Plugin{TemplatesDir: "./templates", SourceDir: pluginDir}
	require.NoError(t, RenderTemplates(p, dst, map[string]string{"Name": "myagent"}))

	got, err := os.ReadFile(filepath.Join(dst, "agent.py"))
	require.NoError(t, err)
	assert.Equal(t, `name = "myagent"`, string(got))

	got2, err := os.ReadFile(filepath.Join(dst, "static.txt"))
	require.NoError(t, err)
	assert.Equal(t, "no template here", string(got2))
}
