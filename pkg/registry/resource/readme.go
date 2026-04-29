package resource

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
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

type readmeOutput struct {
	Body v1alpha1.Readme
}

// maybeRegisterReadmeRoutes auto-registers the readme subresource at
// `/{plural}/{name}/readme` and `/{plural}/{name}/versions/{version}/readme`
// when the kind's typed envelope implements v1alpha1.ObjectWithReadme.
//
// Called from Register[T]; kinds without a Readme field on Spec
// (Provider, Deployment) don't satisfy the interface and silently skip
// readme route registration.
//
// cfg.Authorize gates each handler the same way the regular Register
// routes do — without it, a deny on (Kind, Name) at the row level would
// still leak markdown body via the readme subresource. Verb is "get"
// so role mappings line up with the regular GET handler.
func maybeRegisterReadmeRoutes[T v1alpha1.Object](api huma.API, cfg Config, newObj func() T) {
	if _, ok := any(newObj()).(v1alpha1.ObjectWithReadme); !ok {
		return
	}

	kind := cfg.Kind
	plural := cfg.PluralKind
	if plural == "" {
		plural = strings.ToLower(kind) + "s"
	}
	base := strings.TrimRight(cfg.BasePrefix, "/")
	latestPath := base + "/" + plural + "/{name}/readme"
	versionPath := base + "/" + plural + "/{name}/versions/{version}/readme"

	huma.Register(api, huma.Operation{
		OperationID: "get-latest-" + strings.ToLower(kind) + "-readme",
		Method:      http.MethodGet,
		Path:        latestPath,
		Summary:     fmt.Sprintf("Get the latest %s readme", kind),
	}, func(ctx context.Context, in *readmeLatestInput) (*readmeOutput, error) {
		ns := resolveNamespace(in.Namespace, false)
		name, err := unescapePath("name", in.Name)
		if err != nil {
			return nil, err
		}
		if cfg.Authorize != nil {
			if err := cfg.Authorize(ctx, AuthorizeInput{Verb: "get", Kind: kind, Namespace: ns, Name: name}); err != nil {
				return nil, err
			}
		}
		row, err := cfg.Store.GetLatest(ctx, ns, name)
		if err != nil {
			return nil, mapNotFound(err, kind, ns, name, "")
		}
		return readmeResponseFromRow(row, kind, ns, name, "", newObj)
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-" + strings.ToLower(kind) + "-readme",
		Method:      http.MethodGet,
		Path:        versionPath,
		Summary:     fmt.Sprintf("Get a %s readme by name and version", kind),
	}, func(ctx context.Context, in *readmeVersionInput) (*readmeOutput, error) {
		ns := resolveNamespace(in.Namespace, false)
		name, err := unescapePath("name", in.Name)
		if err != nil {
			return nil, err
		}
		version, err := unescapePath("version", in.Version)
		if err != nil {
			return nil, err
		}
		if cfg.Authorize != nil {
			if err := cfg.Authorize(ctx, AuthorizeInput{Verb: "get", Kind: kind, Namespace: ns, Name: name, Version: version}); err != nil {
				return nil, err
			}
		}
		row, err := cfg.Store.Get(ctx, ns, name, version)
		if err != nil {
			return nil, mapNotFound(err, kind, ns, name, version)
		}
		return readmeResponseFromRow(row, kind, ns, name, version, newObj)
	})
}

func readmeResponseFromRow[T v1alpha1.Object](
	row *v1alpha1.RawObject,
	kind, namespace, name, version string,
	newObj func() T,
) (*readmeOutput, error) {
	obj, err := v1alpha1.EnvelopeFromRaw(newObj, row, kind)
	if err != nil {
		return nil, huma.Error500InternalServerError("decode "+kind, err)
	}

	// maybeRegisterReadmeRoutes already verified the type satisfies
	// ObjectWithReadme, so the assertion here is guaranteed to succeed.
	withReadme := any(obj).(v1alpha1.ObjectWithReadme)
	readme := withReadme.GetReadme()
	if !readme.HasContent() {
		if version == "" {
			return nil, huma.Error404NotFound(fmt.Sprintf("%s %q/%q readme not found", kind, namespace, name))
		}
		return nil, huma.Error404NotFound(fmt.Sprintf("%s %q/%q@%q readme not found", kind, namespace, name, version))
	}
	return &readmeOutput{Body: *readme}, nil
}
