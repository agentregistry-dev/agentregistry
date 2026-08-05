// Package pluginmarketplace wires the read-only Claude Code marketplace.json
// compatibility endpoint. It re-exposes AgentRegistry's Plugin resources —
// resolved to a concrete git commit pin — in the marketplace.json shape
// (code.claude.com/docs/en/plugin-marketplaces) so a bare URL to this
// endpoint can be registered directly with `claude plugin marketplace add`.
//
// Surface (mounted at `{prefix}/plugin-marketplace`, prefix empty by default):
//   - GET /plugin-marketplace/marketplace.json   the full flattened catalogue
//
// Read-only: there is no publish/write path. The handler walks every Plugin
// row across every namespace (paginating internally, transparent to the
// client) and translates each via pkg/pluginmarketplace, silently skipping
// anything not yet resolved or resolved to an unsupported source type (OCI) —
// the marketplace.json document never contains a partial/broken entry.
// It honors the same optional per-kind hooks as the native read path (see
// Config: ListFilter scopes the catalogue). The endpoint is off by default
// regardless (config flag).
package pluginmarketplace

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/logging"
	"github.com/agentregistry-dev/agentregistry/pkg/pluginmarketplace"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/resource"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

var logger = logging.New("plugin-marketplace")

// DefaultMarketplaceName is the `name`/`owner.name` emitted when Config
// doesn't set MarketplaceName.
const DefaultMarketplaceName = "agentregistry"

// pageLimit caps each internal List call while the handler walks the full
// catalogue; it does not limit the size of the resulting document.
const pageLimit = 100

// PluginStore is the narrow read surface this handler needs from the Plugin
// store. *v1alpha1store.Store satisfies it; tests supply a fake.
type PluginStore interface {
	List(ctx context.Context, opts v1alpha1store.ListOpts) ([]*v1alpha1.RawObject, string, error)
}

var _ PluginStore = (*v1alpha1store.Store)(nil)

// Config holds the inputs for Register: the Plugin store the endpoint reads
// from, plus an optional per-kind list-scoping hook. ListFilter defaults to
// nil — the public OSS behavior (full, unfiltered catalogue). Downstream
// builds pass the Plugin entry from crud.PerKindHooks so the compat endpoint
// honors the same RBAC/tenancy gate as the native read path.
type Config struct {
	PathPrefix      string
	MarketplaceName string
	Store           PluginStore
	// ListFilter, when set, injects an ExtraWhere predicate (+args) into the
	// catalogue list query so a downstream RBAC layer can scope which plugins
	// are visible. Same contract as resource.Config.ListFilter: placeholders
	// numbered from $1 relative to the returned args.
	ListFilter func(ctx context.Context, in resource.AuthorizeInput) (string, []any, error)
}

// BasePath returns the mount point of the compatibility API for the given
// optional path prefix. It normalizes the prefix.
func BasePath(pathPrefix string) string {
	prefix := pathPrefix
	if prefix != "" {
		prefix = "/" + strings.Trim(prefix, "/")
	}
	return prefix + "/plugin-marketplace"
}

// Register mounts the marketplace.json compatibility route on api.
// cfg.PathPrefix is prepended to the standard `/plugin-marketplace` base.
func Register(api huma.API, cfg Config) {
	base := BasePath(cfg.PathPrefix)

	huma.Register(api, huma.Operation{
		OperationID: "plugin-marketplace-get",
		Method:      http.MethodGet,
		Path:        base + "/marketplace.json",
		Summary:     "Get the plugin marketplace (Claude Code marketplace.json compatibility)",
		Description: "Read-only listing of resolved plugins in the Claude Code marketplace.json format.",
		Tags:        []string{"plugins"},
	}, getMarketplace(cfg))
}

func getMarketplace(cfg Config) func(context.Context, *struct{}) (*types.Response[pluginmarketplace.MarketplaceResponse], error) {
	return func(ctx context.Context, _ *struct{}) (*types.Response[pluginmarketplace.MarketplaceResponse], error) {
		name := cfg.MarketplaceName
		if name == "" {
			name = DefaultMarketplaceName
		}
		resp := pluginmarketplace.MarketplaceResponse{
			Schema: pluginmarketplace.SchemaURL,
			Name:   name,
			Owner:  pluginmarketplace.Owner{Name: name},
			// Claude Code's marketplace.json parser rejects a null plugins
			// field, so an empty catalogue must still marshal as `[]`.
			Plugins: []pluginmarketplace.PluginEntry{},
		}

		var extraWhere string
		var extraArgs []any
		if cfg.ListFilter != nil {
			frag, fargs, err := cfg.ListFilter(ctx, resource.AuthorizeInput{Verb: "list", Kind: v1alpha1.KindPlugin})
			if err != nil {
				return nil, huma.Error500InternalServerError("authz list filter", err)
			}
			if frag != "" {
				extraWhere = "(" + frag + ")"
				extraArgs = fargs
			}
		}

		seenNames := make(map[string]bool)
		cursor := ""
		for {
			rows, next, err := cfg.Store.List(ctx, v1alpha1store.ListOpts{
				Limit:      pageLimit,
				Cursor:     cursor,
				LatestOnly: true,
				ExtraWhere: extraWhere,
				ExtraArgs:  extraArgs,
			})
			if err != nil {
				if errors.Is(err, v1alpha1store.ErrInvalidCursor) {
					return nil, huma.Error500InternalServerError("plugin marketplace: internal cursor became invalid", err)
				}
				return nil, huma.Error500InternalServerError("list plugins", err)
			}
			for _, raw := range rows {
				p, err := decodePlugin(raw)
				if err != nil {
					return nil, huma.Error500InternalServerError("decode plugin", err)
				}
				entry, err := pluginmarketplace.FromPlugin(p)
				if err != nil {
					// Not-yet-resolved / unsupported-source plugins are silently
					// skipped: the marketplace.json document never contains a
					// partial/broken entry.
					continue
				}
				if seenNames[entry.Name] {
					logger.Warn("plugin marketplace: dropping plugin with a name that collides with an already-emitted entry",
						"name", entry.Name,
						"namespace", p.Metadata.NamespaceOrDefault(),
						"plugin_name", p.Metadata.Name)
					continue
				}
				seenNames[entry.Name] = true
				resp.Plugins = append(resp.Plugins, entry)
			}
			if next == "" {
				break
			}
			cursor = next
		}

		return &types.Response[pluginmarketplace.MarketplaceResponse]{Body: resp}, nil
	}
}

func decodePlugin(raw *v1alpha1.RawObject) (*v1alpha1.Plugin, error) {
	return v1alpha1.EnvelopeFromRaw(func() *v1alpha1.Plugin { return &v1alpha1.Plugin{} }, raw, v1alpha1.KindPlugin)
}
