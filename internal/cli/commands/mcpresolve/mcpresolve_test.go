package mcpresolve

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// fakeFetcher lets tests inject the MCPServer the package would otherwise
// fetch from the live registry. Keeps mcpresolve unit-testable without a
// running server.
type fakeFetcher struct {
	server *v1alpha1.MCPServer
	err    error
}

func (f *fakeFetcher) Fetch(_ context.Context, name, tag string) (*v1alpha1.MCPServer, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.server, nil
}

func TestResolve_RemoteMCP_ReturnsURLAndHeaders(t *testing.T) {
	server := &v1alpha1.MCPServer{
		Metadata: v1alpha1.ObjectMeta{Name: "acme-fetch", Tag: "v1.0.0"},
		Spec: v1alpha1.MCPServerSpec{
			Remote: &v1alpha1.MCPRemote{
				Type: "streamable-http",
				URL:  "https://mcp.acme.com/mcp",
				Headers: []v1alpha1.HTTPHeader{
					{Name: "X-Hello", Value: "world"},
					{Name: "X-Empty", Value: ""}, // dropped — unfilled placeholder
				},
			},
		},
	}
	r, err := Resolve(context.Background(), &fakeFetcher{server: server}, "acme-fetch", "v1.0.0")
	require.NoError(t, err)
	assert.Equal(t, "acme-fetch", r.Name)
	assert.Equal(t, "https://mcp.acme.com/mcp", r.RemoteURL)
	assert.Equal(t, map[string]string{"X-Hello": "world"}, r.RemoteHeaders)
}

func TestResolve_SourceMCP_ReturnsEmptyURL(t *testing.T) {
	server := &v1alpha1.MCPServer{
		Metadata: v1alpha1.ObjectMeta{Name: "acme-source", Tag: "latest"},
		Spec: v1alpha1.MCPServerSpec{
			Source: &v1alpha1.MCPServerSource{}, // any non-nil Source is enough
		},
	}
	r, err := Resolve(context.Background(), &fakeFetcher{server: server}, "acme-source", "latest")
	require.NoError(t, err)
	assert.Equal(t, "acme-source", r.Name)
	assert.Empty(t, r.RemoteURL, "Source-mode MCP should leave RemoteURL empty so callers skip the .env write")
}

func TestResolve_FetcherError_Propagates(t *testing.T) {
	_, err := Resolve(context.Background(), &fakeFetcher{err: errors.New("404 not found")}, "acme-missing", "latest")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "acme-missing")
}
