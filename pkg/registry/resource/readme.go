package resource

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

type readmeLatestInput struct {
	Namespace string `query:"namespace" doc:"Namespace (internal; defaults to 'default')."`
	Name      string `path:"name"`
}

type readmeVersionInput struct {
	Namespace string `query:"namespace" doc:"Namespace (internal; defaults to 'default')."`
	Name      string `path:"name"`
	Version   string `path:"version"`
}

type legacyServerReadmeLatestInput struct {
	ServerName string `path:"serverName"`
}

type legacyServerReadmeVersionInput struct {
	ServerName string `path:"serverName"`
	Version    string `path:"version"`
}

type readmeOutput struct {
	Body v1alpha1.Readme
}

// RegisterReadme wires generic readme subresource routes for one kind.
func RegisterReadme[T v1alpha1.Object](
	api huma.API,
	cfg Config,
	newObj func() T,
	readmeOf func(T) *v1alpha1.Readme,
) {
	plural := cfg.PluralKind
	if plural == "" {
		plural = strings.ToLower(cfg.Kind) + "s"
	}
	base := strings.TrimRight(cfg.BasePrefix, "/")
	// Flat URL shape matching the main Register routes; namespace via
	// ?namespace= query (defaults to "default").
	latestPath := base + "/" + plural + "/{name}/readme"
	versionPath := base + "/" + plural + "/{name}/versions/{version}/readme"

	huma.Register(api, huma.Operation{
		OperationID: "get-latest-" + strings.ToLower(cfg.Kind) + "-readme",
		Method:      http.MethodGet,
		Path:        latestPath,
		Summary:     fmt.Sprintf("Get the latest %s readme", cfg.Kind),
	}, func(ctx context.Context, in *readmeLatestInput) (*readmeOutput, error) {
		ns := resolveNamespace(in.Namespace, false)
		row, err := cfg.Store.GetLatest(ctx, ns, in.Name)
		if err != nil {
			return nil, mapNotFound(err, cfg.Kind, ns, in.Name, "")
		}
		return readmeResponseFromRow(row, cfg.Kind, ns, in.Name, "", newObj, readmeOf)
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-" + strings.ToLower(cfg.Kind) + "-readme",
		Method:      http.MethodGet,
		Path:        versionPath,
		Summary:     fmt.Sprintf("Get a %s readme by name and version", cfg.Kind),
	}, func(ctx context.Context, in *readmeVersionInput) (*readmeOutput, error) {
		ns := resolveNamespace(in.Namespace, false)
		row, err := cfg.Store.Get(ctx, ns, in.Name, in.Version)
		if err != nil {
			return nil, mapNotFound(err, cfg.Kind, ns, in.Name, in.Version)
		}
		return readmeResponseFromRow(row, cfg.Kind, ns, in.Name, in.Version, newObj, readmeOf)
	})
}

// RegisterLegacyServerReadme preserves the historical MCP-server-specific
// readme endpoints while downstream UIs migrate to the generic namespaced
// shape.
func RegisterLegacyServerReadme(api huma.API, basePrefix string, store *v1alpha1store.Store) {
	if store == nil {
		return
	}

	base := strings.TrimRight(basePrefix, "/")

	huma.Register(api, huma.Operation{
		OperationID: "get-server-readme-v0",
		Method:      http.MethodGet,
		Path:        base + "/servers/{serverName}/readme",
		Summary:     "Get server README",
	}, func(ctx context.Context, in *legacyServerReadmeLatestInput) (*readmeOutput, error) {
		row, err := store.GetLatest(ctx, v1alpha1.DefaultNamespace, in.ServerName)
		if err != nil {
			return nil, mapNotFound(err, v1alpha1.KindMCPServer, v1alpha1.DefaultNamespace, in.ServerName, "")
		}
		return readmeResponseFromRow(
			row,
			v1alpha1.KindMCPServer,
			v1alpha1.DefaultNamespace,
			in.ServerName,
			"",
			func() *v1alpha1.MCPServer { return &v1alpha1.MCPServer{} },
			func(obj *v1alpha1.MCPServer) *v1alpha1.Readme { return obj.Spec.Readme },
		)
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-server-version-readme-v0",
		Method:      http.MethodGet,
		Path:        base + "/servers/{serverName}/versions/{version}/readme",
		Summary:     "Get server README for a version",
	}, func(ctx context.Context, in *legacyServerReadmeVersionInput) (*readmeOutput, error) {
		row, err := store.Get(ctx, v1alpha1.DefaultNamespace, in.ServerName, in.Version)
		if err != nil {
			return nil, mapNotFound(err, v1alpha1.KindMCPServer, v1alpha1.DefaultNamespace, in.ServerName, in.Version)
		}
		return readmeResponseFromRow(
			row,
			v1alpha1.KindMCPServer,
			v1alpha1.DefaultNamespace,
			in.ServerName,
			in.Version,
			func() *v1alpha1.MCPServer { return &v1alpha1.MCPServer{} },
			func(obj *v1alpha1.MCPServer) *v1alpha1.Readme { return obj.Spec.Readme },
		)
	})
}

func readmeResponseFromRow[T v1alpha1.Object](
	row *v1alpha1.RawObject,
	kind, namespace, name, version string,
	newObj func() T,
	readmeOf func(T) *v1alpha1.Readme,
) (*readmeOutput, error) {
	obj, err := v1alpha1.EnvelopeFromRaw(newObj, row, kind)
	if err != nil {
		return nil, huma.Error500InternalServerError("decode "+kind, err)
	}

	readme := readmeOf(obj)
	if !readme.HasContent() {
		if version == "" {
			return nil, huma.Error404NotFound(fmt.Sprintf("%s %q/%q readme not found", kind, namespace, name))
		}
		return nil, huma.Error404NotFound(fmt.Sprintf("%s %q/%q@%q readme not found", kind, namespace, name, version))
	}
	return &readmeOutput{Body: *readme}, nil
}
