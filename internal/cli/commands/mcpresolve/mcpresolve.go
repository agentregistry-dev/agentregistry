// Package mcpresolve turns a catalog MCPServer ref (name + tag) into a
// ResolvedMCP describing the URL and headers (if remote) the caller should
// wire into local-dev .env. Sits behind a Fetcher interface so init code
// can drive it from the root registry client while tests inject a fake.
package mcpresolve

import (
	"context"
	"fmt"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// Fetcher abstracts the one registry call this package needs: given a
// catalog ref, return the MCPServer record. The concrete implementation
// delegates to client.GetTyped with the root registry client.
type Fetcher interface {
	Fetch(ctx context.Context, name, tag string) (*v1alpha1.MCPServer, error)
}

// ResolvedMCP carries the fields a caller needs to decide whether (and what)
// to write into .env. For Source-mode records, RemoteURL is empty —
// callers MUST treat that as "skip the .env write." RemoteHeaders is
// pre-flattened to a plain map with empty-Value entries dropped, ready
// to embed in MCP_SERVERS_CONFIG as JSON.
type ResolvedMCP struct {
	Name          string
	RemoteURL     string
	RemoteHeaders map[string]string
}

// Resolve fetches the MCPServer at (name, tag) and returns a ResolvedMCP.
// Errors are wrapped with the ref so callers can surface "--mcp <X>: <err>".
func Resolve(ctx context.Context, f Fetcher, name, tag string) (*ResolvedMCP, error) {
	server, err := f.Fetch(ctx, name, tag)
	if err != nil {
		return nil, fmt.Errorf("--mcp %s: %w", name, err)
	}
	r := &ResolvedMCP{
		Name: server.Metadata.Name,
	}
	if server.Spec.Remote != nil {
		r.RemoteURL = server.Spec.Remote.URL
		r.RemoteHeaders = flattenHeaders(server.Spec.Remote.Headers)
	}
	return r, nil
}

// flattenHeaders turns HTTPHeader rows into a plain map, dropping entries
// with no Value (unfilled placeholders / required-without-default).
// Returns nil when no usable rows survive so JSON omitempty drops the key.
func flattenHeaders(in []v1alpha1.HTTPHeader) map[string]string {
	var out map[string]string
	for _, h := range in {
		if h.Value == "" {
			continue
		}
		if out == nil {
			out = map[string]string{}
		}
		out[h.Name] = h.Value
	}
	return out
}
