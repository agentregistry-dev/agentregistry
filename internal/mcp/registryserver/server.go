// Package registryserver exposes the agentregistry over MCP so Claude +
// other MCP clients can list and fetch resources as typed tools.
//
// Every tool reads through the v1alpha1 generic Store. Structured
// outputs are v1alpha1 envelopes (apiVersion/kind/metadata/spec/status).
package registryserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/agentregistry-dev/agentregistry/internal/version"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/resource"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

// Authorizer gates a read operation for a kind against the caller's session
// (carried on the context). ListFilter returns an ExtraWhere predicate + args
// that scope a list to what the caller may see. Both mirror the per-kind hooks
// the REST resource handlers consult (resource.Config.Authorize /
// resource.Config.ListFilter), so the bridge enforces the same RBAC.
// A nil hook for a kind means no restriction.
type Authorizer = func(ctx context.Context, in resource.AuthorizeInput) error

// ListFilter is the per-kind list-scoping hook; see Authorizer.
type ListFilter = func(ctx context.Context, in resource.AuthorizeInput) (extraWhere string, extraArgs []any, err error)

const (
	defaultPageLimit = 30
	maxPageLimit     = 100
)

// NewServer constructs an MCP server that exposes discovery tools backed
// by v1alpha1 Stores. Tools are namespace-aware; when a tool input omits
// the namespace, the server searches across all namespaces for backward
// compatibility with pre-namespaced clients.
//
// Tool names are preserved across builds (`list_servers` not
// `list_mcpservers`) so saved Claude MCP configs keep working.
func NewServer(
	stores map[string]*v1alpha1store.Store,
	authorizers map[string]Authorizer,
	listFilters map[string]ListFilter,
) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "agentregistry-mcp",
		Version: version.Version,
	}, &mcp.ServerOptions{
		HasTools:   true,
		HasPrompts: true,
	})

	addKindTools(server, stores[v1alpha1.KindAgent], kindTools[*v1alpha1.Agent]{
		Kind:       v1alpha1.KindAgent,
		ListName:   "list_agents",
		GetName:    "get_agent",
		ListDesc:   "List published agents as v1alpha1 envelopes with optional namespace, substring-name, and tag filters.",
		GetDesc:    "Fetch a published agent as a v1alpha1 envelope (defaults to the latest tag).",
		NewObj:     func() *v1alpha1.Agent { return &v1alpha1.Agent{} },
		Authorize:  authorizers[v1alpha1.KindAgent],
		ListFilter: listFilters[v1alpha1.KindAgent],
	})
	addKindTools(server, stores[v1alpha1.KindMCPServer], kindTools[*v1alpha1.MCPServer]{
		Kind:       v1alpha1.KindMCPServer,
		ListName:   "list_servers",
		GetName:    "get_server",
		ListDesc:   "List published MCP servers as v1alpha1 envelopes with optional namespace, substring-name, and tag filters.",
		GetDesc:    "Fetch a published MCP server as a v1alpha1 envelope (defaults to the latest tag).",
		NewObj:     func() *v1alpha1.MCPServer { return &v1alpha1.MCPServer{} },
		Authorize:  authorizers[v1alpha1.KindMCPServer],
		ListFilter: listFilters[v1alpha1.KindMCPServer],
	})
	addKindTools(server, stores[v1alpha1.KindSkill], kindTools[*v1alpha1.Skill]{
		Kind:       v1alpha1.KindSkill,
		ListName:   "list_skills",
		GetName:    "get_skill",
		ListDesc:   "List published skills as v1alpha1 envelopes with optional namespace, substring-name, and tag filters.",
		GetDesc:    "Fetch a published skill as a v1alpha1 envelope (defaults to the latest tag).",
		NewObj:     func() *v1alpha1.Skill { return &v1alpha1.Skill{} },
		Authorize:  authorizers[v1alpha1.KindSkill],
		ListFilter: listFilters[v1alpha1.KindSkill],
	})
	addKindTools(server, stores[v1alpha1.KindPrompt], kindTools[*v1alpha1.Prompt]{
		Kind:       v1alpha1.KindPrompt,
		ListName:   "list_prompts",
		GetName:    "get_prompt",
		ListDesc:   "List published prompts as v1alpha1 envelopes with optional namespace, substring-name, and tag filters.",
		GetDesc:    "Fetch a published prompt as a v1alpha1 envelope (defaults to the latest tag).",
		NewObj:     func() *v1alpha1.Prompt { return &v1alpha1.Prompt{} },
		Authorize:  authorizers[v1alpha1.KindPrompt],
		ListFilter: listFilters[v1alpha1.KindPrompt],
	})
	addKindTools(server, stores[v1alpha1.KindModel], kindTools[*v1alpha1.Model]{
		Kind:       v1alpha1.KindModel,
		ListName:   "list_models",
		GetName:    "get_model",
		ListDesc:   "List published models as v1alpha1 envelopes with optional namespace, substring-name, and tag filters.",
		GetDesc:    "Fetch a published model as a v1alpha1 envelope (defaults to the latest tag).",
		NewObj:     func() *v1alpha1.Model { return &v1alpha1.Model{} },
		Authorize:  authorizers[v1alpha1.KindModel],
		ListFilter: listFilters[v1alpha1.KindModel],
	})
	addKindTools(server, stores[v1alpha1.KindPlugin], kindTools[*v1alpha1.Plugin]{
		Kind:       v1alpha1.KindPlugin,
		ListName:   "list_plugins",
		GetName:    "get_plugin",
		ListDesc:   "List published plugins as v1alpha1 envelopes with optional namespace, substring-name, and tag filters.",
		GetDesc:    "Fetch a published plugin as a v1alpha1 envelope (defaults to the latest tag).",
		NewObj:     func() *v1alpha1.Plugin { return &v1alpha1.Plugin{} },
		Authorize:  authorizers[v1alpha1.KindPlugin],
		ListFilter: listFilters[v1alpha1.KindPlugin],
	})
	addKindTools(server, stores[v1alpha1.KindDeployment], kindTools[*v1alpha1.Deployment]{
		Kind:       v1alpha1.KindDeployment,
		Mutable:    true,
		ListName:   "list_deployments",
		GetName:    "get_deployment",
		ListDesc:   "List deployments as v1alpha1 envelopes with optional namespace and substring-name filters.",
		GetDesc:    "Fetch a deployment as a v1alpha1 envelope by namespace/name.",
		NewObj:     func() *v1alpha1.Deployment { return &v1alpha1.Deployment{} },
		Authorize:  authorizers[v1alpha1.KindDeployment],
		ListFilter: listFilters[v1alpha1.KindDeployment],
	})
	addKindTools(server, stores[v1alpha1.KindRuntime], kindTools[*v1alpha1.Runtime]{
		Kind:       v1alpha1.KindRuntime,
		Mutable:    true,
		ListName:   "list_runtimes",
		GetName:    "get_runtime",
		ListDesc:   "List runtimes as v1alpha1 envelopes with optional namespace and substring-name filters.",
		GetDesc:    "Fetch a runtime as a v1alpha1 envelope by namespace/name.",
		NewObj:     func() *v1alpha1.Runtime { return &v1alpha1.Runtime{} },
		Authorize:  authorizers[v1alpha1.KindRuntime],
		ListFilter: listFilters[v1alpha1.KindRuntime],
	})
	addMetaTools(server)
	addServerPrompts(server)

	return server
}

// kindTools bundles the per-kind inputs the generic addKindTools helper
// needs to register `list_X` + `get_X` for a v1alpha1 kind. Tool names
// are explicit (not derived from Kind) because they are user-facing in
// Claude - `list_servers` is kept, not renamed to `list_mcpservers`.
type kindTools[T v1alpha1.Object] struct {
	Kind     string
	Mutable  bool
	ListName string
	GetName  string
	ListDesc string
	GetDesc  string
	NewObj   func() T
	// Authorize + ListFilter are per-kind RBAC hooks, nil when the build wires no authz for this kind.
	Authorize  Authorizer
	ListFilter ListFilter
}

// Explicit schemas match custom JSON marshalers rather than their Go storage
// shapes, which would reject valid structured tool output.
func outputSchemaFor[T any]() *jsonschema.Schema {
	rt := reflect.TypeFor[T]()
	if rt.Kind() == reflect.Pointer {
		rt = rt.Elem()
	}
	s, err := jsonschema.ForType(rt, &jsonschema.ForOptions{
		TypeSchemas: map[reflect.Type]*jsonschema.Schema{
			reflect.TypeFor[json.RawMessage](): {Types: []string{
				"null", "boolean", "object", "array", "number", "string",
			}},
			reflect.TypeFor[v1alpha1.CommandsField](): {Types: []string{
				"null", "object", "array", "string",
			}},
		},
	})
	if err != nil {
		panic(fmt.Sprintf("registryserver: output schema for %v: %v", rt, err))
	}
	return s
}

// addKindTools registers list_X + get_X MCP tools for a v1alpha1 kind.
// Nil store is a no-op so bootstrap can wire every kind unconditionally
// and skip ones the backend doesn't expose.
func addKindTools[T v1alpha1.Object](server *mcp.Server, store *v1alpha1store.Store, cfg kindTools[T]) {
	if store == nil {
		return
	}
	listTool := &mcp.Tool{
		Name:         cfg.ListName,
		Description:  cfg.ListDesc,
		OutputSchema: outputSchemaFor[listOutput[T]](),
	}
	list := func(ctx context.Context, args listInput) (*mcp.CallToolResult, listOutput[T], error) {
		raws, next, err := runList(ctx, store, cfg.Kind, cfg.Authorize, cfg.ListFilter, args)
		if err != nil {
			return nil, listOutput[T]{}, err
		}
		items, err := envelopesFromRows(raws, cfg.Kind, cfg.NewObj, args.Search)
		if err != nil {
			return nil, listOutput[T]{}, err
		}
		return nil, listOutput[T]{Items: items, NextCursor: next, Count: len(items)}, nil
	}
	if !cfg.Mutable {
		mcp.AddTool(server, listTool, func(ctx context.Context, _ *mcp.CallToolRequest, args listInput) (*mcp.CallToolResult, listOutput[T], error) {
			return list(ctx, args)
		})
	} else {
		mcp.AddTool(server, listTool, func(ctx context.Context, _ *mcp.CallToolRequest, args mutableListInput) (*mcp.CallToolResult, listOutput[T], error) {
			return list(ctx, listInput{
				Namespace: args.Namespace,
				Cursor:    args.Cursor,
				Limit:     args.Limit,
				Search:    args.Search,
			})
		})
	}

	getTool := &mcp.Tool{
		Name:         cfg.GetName,
		Description:  cfg.GetDesc,
		OutputSchema: outputSchemaFor[T](),
	}
	if !cfg.Mutable {
		mcp.AddTool(server, getTool, func(ctx context.Context, _ *mcp.CallToolRequest, args getByRefInput) (*mcp.CallToolResult, T, error) {
			return getEnvelope(ctx, store, cfg.Kind, cfg.Authorize, args, cfg.NewObj)
		})
	} else {
		mcp.AddTool(server, getTool, func(ctx context.Context, _ *mcp.CallToolRequest, args getByNameInput) (*mcp.CallToolResult, T, error) {
			return getEnvelope(ctx, store, cfg.Kind, cfg.Authorize, getByRefInput{
				Namespace: args.Namespace,
				Name:      args.Name,
			}, cfg.NewObj)
		})
	}
}

// listInput is the shared shape for list_* tools. Search is a
// case-insensitive substring filter applied server-side against
// metadata.name after Store.List returns a page.
type listInput struct {
	Namespace string `json:"namespace,omitempty" doc:"Filter by namespace (empty = all namespaces)"`
	Cursor    string `json:"cursor,omitempty"    doc:"Pagination cursor returned by a previous call"`
	Limit     int    `json:"limit,omitempty"     doc:"Max items (1-100, default 30)"`
	Search    string `json:"search,omitempty"    doc:"Case-insensitive substring filter on metadata.name"`
	Tag       string `json:"tag,omitempty"       doc:"Exact tag; empty returns every tag"`
}

type mutableListInput struct {
	Namespace string `json:"namespace,omitempty" doc:"Filter by namespace (empty = all namespaces)"`
	Cursor    string `json:"cursor,omitempty"    doc:"Pagination cursor returned by a previous call"`
	Limit     int    `json:"limit,omitempty"     doc:"Max items (1-100, default 30)"`
	Search    string `json:"search,omitempty"    doc:"Case-insensitive substring filter on metadata.name"`
}

type getByRefInput struct {
	Namespace string `json:"namespace,omitempty" doc:"Namespace (empty defaults to 'default')"`
	Name      string `json:"name"                doc:"Resource name"    required:"true"`
	Tag       string `json:"tag,omitempty"       doc:"Exact tag; empty or 'latest' returns the literal latest tag"`
}

type getByNameInput struct {
	Namespace string `json:"namespace,omitempty" doc:"Namespace (empty defaults to 'default')"`
	Name      string `json:"name"                doc:"Resource name"    required:"true"`
}

// listOutput is the generic envelope every list_* tool returns. Items
// are fully-typed v1alpha1 envelopes.
type listOutput[T v1alpha1.Object] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"nextCursor,omitempty"`
	Count      int    `json:"count"`
}

// Deployment note: only read tools (list + get) are exposed via MCP.
// Create + delete equivalents live on the v1alpha1 apply surface at
// /v0/deployments/{name}?namespace={ns} — MCP clients that need to
// deploy should PUT or DELETE against that HTTP path directly.

func addMetaTools(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "registry_health",
		Description: "Simple health check for the registry MCP bridge",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, map[string]string, error) {
		_ = ctx
		return nil, map[string]string{"status": "ok"}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "registry_version",
		Description: "Return registry build metadata",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, map[string]string, error) {
		return nil, map[string]string{
			"version":    version.Version,
			"serverName": "agentregistry-mcp",
		}, nil
	})
}

// -----------------------------------------------------------------------------
// Internal glue — generic list + get helpers shared across kinds.
// -----------------------------------------------------------------------------

func runList(
	ctx context.Context,
	store *v1alpha1store.Store,
	kind string,
	authorize Authorizer,
	listFilter ListFilter,
	args listInput,
) ([]*v1alpha1.RawObject, string, error) {
	namespace := strings.TrimSpace(args.Namespace)
	if authorize != nil {
		if err := authorize(ctx, resource.AuthorizeInput{Verb: "list", Kind: kind, Namespace: namespace}); err != nil {
			return nil, "", err
		}
	}
	opts := v1alpha1store.ListOpts{
		Namespace: namespace,
		Limit:     clampLimit(args.Limit),
		Cursor:    args.Cursor,
	}
	tag := strings.TrimSpace(args.Tag)
	if strings.EqualFold(tag, "latest") {
		opts.LatestOnly = true
	} else if tag != "" {
		opts.Tag = tag
	}
	if listFilter != nil {
		extraWhere, extraArgs, err := listFilter(ctx, resource.AuthorizeInput{Verb: "list", Kind: kind, Namespace: namespace})
		if err != nil {
			return nil, "", err
		}
		opts.ExtraWhere = extraWhere
		opts.ExtraArgs = extraArgs
	}
	raws, next, err := store.List(ctx, opts)
	if err != nil {
		return nil, "", fmt.Errorf("list: %w", err)
	}
	return raws, next, nil
}

// envelopesFromRows materializes typed envelopes from RawObject rows and
// applies the optional substring-name filter. Returns the items that
// survived the filter; callers set Count from len(items).
func envelopesFromRows[T v1alpha1.Object](
	raws []*v1alpha1.RawObject,
	kind string,
	newObj func() T,
	search string,
) ([]T, error) {
	needle := strings.ToLower(strings.TrimSpace(search))
	out := make([]T, 0, len(raws))
	for _, raw := range raws {
		if needle != "" && !strings.Contains(strings.ToLower(raw.Metadata.Name), needle) {
			continue
		}
		obj, err := v1alpha1.EnvelopeFromRaw(newObj, raw, kind)
		if err != nil {
			return nil, err
		}
		out = append(out, obj)
	}
	return out, nil
}

func getEnvelope[T v1alpha1.Object](
	ctx context.Context,
	store *v1alpha1store.Store,
	kind string,
	authorize Authorizer,
	args getByRefInput,
	newObj func() T,
) (*mcp.CallToolResult, T, error) {
	if strings.TrimSpace(args.Name) == "" {
		var zero T
		return nil, zero, errors.New("name is required")
	}
	namespace := strings.TrimSpace(args.Namespace)
	if namespace == "" {
		namespace = v1alpha1.DefaultNamespace
	}
	tag := strings.TrimSpace(args.Tag)
	if authorize != nil {
		if err := authorize(ctx, resource.AuthorizeInput{Verb: "get", Kind: kind, Namespace: namespace, Name: args.Name, Tag: tag}); err != nil {
			var zero T
			return nil, zero, err
		}
	}

	var (
		raw *v1alpha1.RawObject
		err error
	)
	if tag == "" || strings.EqualFold(tag, "latest") {
		raw, err = store.GetLatest(ctx, namespace, args.Name)
	} else {
		raw, err = store.Get(ctx, namespace, args.Name, tag)
	}
	if err != nil {
		var zero T
		if errors.Is(err, pkgdb.ErrNotFound) {
			return nil, zero, fmt.Errorf("%s %q/%q not found", kind, namespace, args.Name)
		}
		return nil, zero, fmt.Errorf("fetch %s: %w", kind, err)
	}
	obj, err := v1alpha1.EnvelopeFromRaw(newObj, raw, kind)
	if err != nil {
		var zero T
		return nil, zero, fmt.Errorf("decode %s: %w", kind, err)
	}
	return nil, obj, nil
}

func clampLimit(limit int) int {
	if limit <= 0 {
		return defaultPageLimit
	}
	if limit > maxPageLimit {
		return maxPageLimit
	}
	return limit
}

// addServerPrompts registers MCP prompts that describe how to use the
// registry's tools. These prompts are user-facing; they get listed in
// Claude's prompt picker, so intent matters more than wording.
func addServerPrompts(server *mcp.Server) {
	server.AddPrompt(&mcp.Prompt{
		Name:        "search_registry",
		Description: "Search the agent registry for MCP servers, agents, skills, prompts, plugins, deployments, runtimes, or models by keyword",
		Arguments: []*mcp.PromptArgument{
			{Name: "query", Description: "Search term or keyword", Required: true},
			{Name: "type", Description: "Resource type to search: servers, agents, skills, prompts, plugins, deployments, runtimes, or models (default: all)"},
		},
	}, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		query := req.Params.Arguments["query"]
		resourceType := req.Params.Arguments["type"]

		instruction := "Search the agent registry for \"" + query + "\""
		if resourceType != "" {
			instruction += " (filter to " + resourceType + " only)"
		}
		instruction += ". Use the appropriate list tool (list_servers, list_agents, list_skills, list_prompts, list_plugins, list_deployments, list_runtimes, list_models) with the search parameter. Summarize what you find including names, descriptions, and tags."

		return &mcp.GetPromptResult{
			Description: "Search the registry for resources matching a query",
			Messages: []*mcp.PromptMessage{
				{Role: "user", Content: &mcp.TextContent{Text: instruction}},
			},
		}, nil
	})

	server.AddPrompt(&mcp.Prompt{
		Name:        "registry_overview",
		Description: "Get an overview of everything available in the agent registry",
	}, func(_ context.Context, _ *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return &mcp.GetPromptResult{
			Description: "Overview of registry contents",
			Messages: []*mcp.PromptMessage{
				{Role: "user", Content: &mcp.TextContent{
					Text: "Give me an overview of what's available in the agent registry. " +
						"Use list_servers, list_agents, list_skills, list_prompts, list_plugins, and list_models to see what's published. " +
						"Use list_deployments to see what's currently deployed. And use list_runtimes to view available runtimes for deployment. " +
						"Summarize the results in a clear table format showing name, description, and tag for each resource type.",
				}},
			},
		}, nil
	})
}
