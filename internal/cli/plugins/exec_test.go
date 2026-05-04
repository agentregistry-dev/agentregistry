package plugins

import (
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
