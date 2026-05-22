package declarative

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/internal/cli/frameworks"
)

func TestShouldIgnore_GitDir(t *testing.T) {
	assert.True(t, shouldIgnore("/proj/.git/refs/main"))
	assert.True(t, shouldIgnore("/proj/.gitignore"))
	assert.False(t, shouldIgnore("/proj/agent.py"))
}

// runWithWatch must render {{.Image}} and {{.Port}} for MCP frameworks whose
// run command references them directly (e.g. fastmcp-python: `docker run -p
// {{.Port}}:{{.Port}} {{.Image}}`). Regression for a bug where the watch
// path's vars map omitted Image/Port and the template rendered "<no value>".
func TestRunWithWatch_RendersImageAndPort(t *testing.T) {
	p := &frameworks.Framework{
		Name:      "fastmcp-python",
		Type:      "mcp",
		SourceDir: t.TempDir(),
		Run: frameworks.Command{
			Command: []string{"docker", "run", "--rm", "-p", "{{.Port}}:{{.Port}}", "{{.Image}}"},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var buf bytes.Buffer
	err := runWithWatch(ctx, &buf, t.TempDir(), p, "localhost:5001/test:latest", 3000, nil, true /*dryRun*/, false /*inspector*/)
	require.NoError(t, err)

	out := buf.String()
	assert.NotContains(t, out, "<no value>", "watch path must populate Image and Port template vars")
	assert.Contains(t, out, "3000:3000")
	assert.Contains(t, out, "localhost:5001/test:latest")
	assert.Contains(t, out, "--name arctl-watch-", "docker run argv must be named so we can docker rm -f on restart")
}

// --watch + --inspector should compose: today the watch path returns before
// the inspector code in runProject, so without explicit plumbing the
// --inspector flag was silently dropped. This locks in the narrative path
// — actual subprocess launch happens only outside dryRun.
func TestRunWithWatch_InspectorComposes(t *testing.T) {
	p := &frameworks.Framework{
		Name:      "fastmcp-python",
		Type:      "mcp",
		SourceDir: t.TempDir(),
		Run: frameworks.Command{
			Command: []string{"docker", "run", "--rm", "-p", "{{.Port}}:{{.Port}}", "{{.Image}}"},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var buf bytes.Buffer
	err := runWithWatch(ctx, &buf, t.TempDir(), p, "img:latest", 3000, nil, true /*dryRun*/, true /*inspector*/)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "MCP Inspector", "inspector narration must appear when --watch --inspector is set")
	assert.Contains(t, out, "http://localhost:3000/mcp")
}

func TestInjectDockerName(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "docker run gets name injected after 'run'",
			in:   []string{"docker", "run", "--rm", "-p", "3000:3000", "img:latest"},
			want: []string{"docker", "run", "--name", "arctl-watch-x", "--rm", "-p", "3000:3000", "img:latest"},
		},
		{
			name: "docker compose unchanged (not docker run)",
			in:   []string{"docker", "compose", "up", "--build"},
			want: []string{"docker", "compose", "up", "--build"},
		},
		{
			name: "non-docker unchanged",
			in:   []string{"podman", "run", "--rm", "img:latest"},
			want: []string{"podman", "run", "--rm", "img:latest"},
		},
		{
			name: "too short unchanged",
			in:   []string{"docker"},
			want: []string{"docker"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, injectDockerName(tc.in, "arctl-watch-x"))
		})
	}
}
