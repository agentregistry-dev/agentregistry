// Package mcpregistry wires the read-only MCP Registry v0.1 compatibility
// endpoints. It re-exposes AgentRegistry's MCPServer resources in the official
// `server.json` shape (github.com/modelcontextprotocol/registry) so that
// registry-aware clients — VS Code's MCP gallery and other subregistry
// consumers — can discover servers again after the native API moved off the
// upstream contract.
//
// Surface (mounted at `{prefix}/v0.1`, prefix empty by default):
//   - GET /v0.1/servers                               list (cursor paginated)
//   - GET /v0.1/servers/{serverName}/versions         all versions of a server
//   - GET /v0.1/servers/{serverName}/versions/{ver}   one version ("latest" ok)
//
// Read-only: there is no publish/write path. The handler reads MCPServer rows
// straight from the store across every namespace (the catalogue is flat and
// anonymous by design) and translates them via pkg/mcpregistry. It does NOT
// invoke per-kind authz/list filters, so downstream deployments that gate reads
// with RBAC should disable the endpoint (config flag) rather than rely on it.
package mcpregistry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/mcpregistry"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

// maxLimit caps the page size a client can request, matching the upstream
// registry's ceiling. Zero (unset) falls through to the store default.
const maxLimit = 100

// ServerStore is the narrow read surface this handler needs from the MCPServer
// store. *v1alpha1store.Store satisfies it; tests supply a fake.
type ServerStore interface {
	List(ctx context.Context, opts v1alpha1store.ListOpts) ([]*v1alpha1.RawObject, string, error)
	GetLatest(ctx context.Context, namespace, name string) (*v1alpha1.RawObject, error)
	Get(ctx context.Context, namespace, name, tag string) (*v1alpha1.RawObject, error)
}

var _ ServerStore = (*v1alpha1store.Store)(nil)

// Register mounts the v0.1 compatibility routes on api. pathPrefix is prepended
// to the standard `/v0.1` base (empty serves the spec paths at root); store is
// the MCPServer store the endpoints read from.
func Register(api huma.API, pathPrefix string, store ServerStore) {
	base := pathPrefix + "/v0.1"

	huma.Register(api, huma.Operation{
		OperationID: "mcp-registry-list-servers",
		Method:      http.MethodGet,
		Path:        base + "/servers",
		Summary:     "List MCP servers (MCP Registry v0.1 compatibility)",
		Description: "Read-only listing of registered MCP servers in the official MCP Registry server.json format.",
		Tags:        []string{"servers"},
	}, listServers(store))

	huma.Register(api, huma.Operation{
		OperationID: "mcp-registry-list-server-versions",
		Method:      http.MethodGet,
		Path:        base + "/servers/{serverName}/versions",
		Summary:     "List versions of an MCP server (MCP Registry v0.1 compatibility)",
		Tags:        []string{"servers"},
	}, listServerVersions(store))

	huma.Register(api, huma.Operation{
		OperationID: "mcp-registry-get-server-version",
		Method:      http.MethodGet,
		Path:        base + "/servers/{serverName}/versions/{version}",
		Summary:     "Get a single MCP server version (MCP Registry v0.1 compatibility)",
		Tags:        []string{"servers"},
	}, getServerVersion(store))
}

type listServersInput struct {
	Cursor         string `query:"cursor" doc:"Opaque pagination cursor from a prior response."`
	Limit          int    `query:"limit" doc:"Max servers to return (capped at 100)."`
	Search         string `query:"search" doc:"Substring match on the server name."`
	UpdatedSince   string `query:"updated_since" doc:"RFC3339 timestamp; only servers updated at or after this time."`
	Version        string `query:"version" doc:"'latest' (default) or a specific version tag."`
	IncludeDeleted bool   `query:"include_deleted" doc:"Include servers pending deletion."`
}

type serverListOutput struct {
	Body mcpregistry.ServerListResponse
}

func listServers(store ServerStore) func(context.Context, *listServersInput) (*serverListOutput, error) {
	return func(ctx context.Context, in *listServersInput) (*serverListOutput, error) {
		opts := v1alpha1store.ListOpts{
			// Empty namespace flattens the catalogue across every namespace.
			Limit:              clampLimit(in.Limit),
			Cursor:             in.Cursor,
			IncludeTerminating: in.IncludeDeleted,
		}
		switch in.Version {
		case "", "latest":
			opts.LatestOnly = true
		default:
			opts.Tag = in.Version
		}

		preds := make([]string, 0, 2)
		args := make([]any, 0, 2)
		if in.Search != "" {
			args = append(args, "%"+in.Search+"%")
			preds = append(preds, fmt.Sprintf("name ILIKE $%d", len(args)))
		}
		if in.UpdatedSince != "" {
			ts, err := time.Parse(time.RFC3339, in.UpdatedSince)
			if err != nil {
				return nil, huma.Error400BadRequest(fmt.Sprintf("invalid updated_since (want RFC3339): %v", err))
			}
			args = append(args, ts)
			preds = append(preds, fmt.Sprintf("updated_at >= $%d", len(args)))
		}
		if len(preds) > 0 {
			opts.ExtraWhere = strings.Join(preds, " AND ")
			opts.ExtraArgs = args
		}

		rows, next, err := store.List(ctx, opts)
		if err != nil {
			if errors.Is(err, v1alpha1store.ErrInvalidCursor) {
				return nil, huma.Error400BadRequest(fmt.Sprintf("invalid cursor: %v", err))
			}
			return nil, huma.Error500InternalServerError("list MCP servers", err)
		}
		servers, err := translateRows(rows)
		if err != nil {
			return nil, err
		}
		out := &serverListOutput{}
		out.Body = mcpregistry.ServerListResponse{
			Servers:  servers,
			Metadata: mcpregistry.ListMetadata{NextCursor: next, Count: len(servers)},
		}
		return out, nil
	}
}

type listServerVersionsInput struct {
	ServerName     string `path:"serverName" doc:"URL-encoded '<namespace>/<name>' server name."`
	Cursor         string `query:"cursor"`
	Limit          int    `query:"limit"`
	IncludeDeleted bool   `query:"include_deleted"`
}

func listServerVersions(store ServerStore) func(context.Context, *listServerVersionsInput) (*serverListOutput, error) {
	return func(ctx context.Context, in *listServerVersionsInput) (*serverListOutput, error) {
		ns, name, err := parseServerNameParam(in.ServerName)
		if err != nil {
			return nil, err
		}
		rows, next, err := store.List(ctx, v1alpha1store.ListOpts{
			Namespace:          ns,
			Limit:              clampLimit(in.Limit),
			Cursor:             in.Cursor,
			IncludeTerminating: in.IncludeDeleted,
			ExtraWhere:         "name = $1",
			ExtraArgs:          []any{name},
		})
		if err != nil {
			if errors.Is(err, v1alpha1store.ErrInvalidCursor) {
				return nil, huma.Error400BadRequest(fmt.Sprintf("invalid cursor: %v", err))
			}
			return nil, huma.Error500InternalServerError("list MCP server versions", err)
		}
		// An empty first page means the server name doesn't exist at all.
		if len(rows) == 0 && in.Cursor == "" {
			return nil, huma.Error404NotFound(fmt.Sprintf("MCP server %q not found", in.ServerName))
		}
		servers, err := translateRows(rows)
		if err != nil {
			return nil, err
		}
		out := &serverListOutput{}
		out.Body = mcpregistry.ServerListResponse{
			Servers:  servers,
			Metadata: mcpregistry.ListMetadata{NextCursor: next, Count: len(servers)},
		}
		return out, nil
	}
}

type getServerVersionInput struct {
	ServerName string `path:"serverName" doc:"URL-encoded '<namespace>/<name>' server name."`
	Version    string `path:"version" doc:"A specific version tag, or 'latest'."`
}

type serverOutput struct {
	Body mcpregistry.ServerResponse
}

func getServerVersion(store ServerStore) func(context.Context, *getServerVersionInput) (*serverOutput, error) {
	return func(ctx context.Context, in *getServerVersionInput) (*serverOutput, error) {
		ns, name, err := parseServerNameParam(in.ServerName)
		if err != nil {
			return nil, err
		}
		version, err := url.PathUnescape(in.Version)
		if err != nil {
			return nil, huma.Error400BadRequest(fmt.Sprintf("invalid version path segment: %v", err))
		}

		var raw *v1alpha1.RawObject
		if version == "" || version == "latest" {
			raw, err = store.GetLatest(ctx, ns, name)
		} else {
			raw, err = store.Get(ctx, ns, name, version)
		}
		if err != nil {
			if errors.Is(err, pkgdb.ErrNotFound) {
				return nil, huma.Error404NotFound(fmt.Sprintf("MCP server %q version %q not found", in.ServerName, version))
			}
			return nil, huma.Error500InternalServerError("get MCP server", err)
		}
		ms, err := decodeMCPServer(raw)
		if err != nil {
			return nil, huma.Error500InternalServerError("decode MCP server", err)
		}
		out := &serverOutput{}
		out.Body = mcpregistry.FromMCPServer(ms)
		return out, nil
	}
}

// parseServerNameParam unescapes a `{serverName}` path capture (Huma keeps
// captures raw, so a `%2F`-escaped slash arrives verbatim) and splits it into
// the AgentRegistry namespace + name.
func parseServerNameParam(raw string) (namespace, name string, err error) {
	decoded, uerr := url.PathUnescape(raw)
	if uerr != nil {
		return "", "", huma.Error400BadRequest(fmt.Sprintf("invalid serverName path segment: %v", uerr))
	}
	ns, n, perr := mcpregistry.ParseServerName(decoded)
	if perr != nil {
		return "", "", huma.Error404NotFound(perr.Error())
	}
	return ns, n, nil
}

// translateRows decodes and translates a page of raw rows into server
// responses.
func translateRows(rows []*v1alpha1.RawObject) ([]mcpregistry.ServerResponse, error) {
	servers := make([]mcpregistry.ServerResponse, 0, len(rows))
	for _, raw := range rows {
		ms, err := decodeMCPServer(raw)
		if err != nil {
			return nil, huma.Error500InternalServerError("decode MCP server", err)
		}
		servers = append(servers, mcpregistry.FromMCPServer(ms))
	}
	return servers, nil
}

func decodeMCPServer(raw *v1alpha1.RawObject) (*v1alpha1.MCPServer, error) {
	return v1alpha1.EnvelopeFromRaw(func() *v1alpha1.MCPServer { return &v1alpha1.MCPServer{} }, raw, v1alpha1.KindMCPServer)
}

func clampLimit(limit int) int {
	if limit > maxLimit {
		return maxLimit
	}
	if limit < 0 {
		return 0
	}
	return limit
}
