// Package mcpresolve turns a catalog MCPServer ref (name + tag) into a
// ResolvedMCP describing the URL and headers (if remote) the caller should
// wire into local-dev .env. Sits behind a Fetcher interface so init code
// can drive it from the live apiClient while tests inject a fake.
package mcpresolve

import (
	"context"
	"fmt"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// Fetcher abstracts the one registry call this package needs: given a
// catalog ref, return the MCPServer record. The concrete implementation
// (in init.go) delegates to client.GetTyped against the package-level
// apiClient.
type Fetcher interface {
	Fetch(ctx context.Context, name, tag string) (*v1alpha1.MCPServer, error)
}

// ResolvedMCP carries the fields a caller needs to decide whether (and what)
// to write into .env. For Source-mode records, RemoteURL is empty —
// callers MUST treat that as "skip the .env write."
type ResolvedMCP struct {
	Name          string
	Tag           string
	RemoteURL     string
	RemoteHeaders []v1alpha1.MCPKeyValueInput
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
		Tag:  server.Metadata.Tag,
	}
	if server.Spec.Remote != nil {
		r.RemoteURL = server.Spec.Remote.URL
		r.RemoteHeaders = server.Spec.Remote.Headers
	}
	return r, nil
}
