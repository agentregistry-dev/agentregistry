// Package importpipeline owns POST /v0/import — the multi-doc YAML
// import endpoint that runs each decoded document through the
// pre-constructed importer.Importer (validation + scanner enrichment
// + Upsert) and returns per-document results.
//
// Distinct from the per-kind CRUD bindings in v1alpha1crud and from
// the in-package POST /v0/apply (pkg/registry/resource): apply
// short-circuits to plain Upsert without scanner runs, while
// importpipeline always passes through the importer's enrichment
// pipeline so scanner annotations + findings rows land alongside
// the persisted spec.
package importpipeline

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/importer"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/resource"
)

// Config wires POST {BasePrefix}/import. Importer is the
// pre-constructed importer.Importer (scanners + findings store +
// resolver injected at server boot); the handler forwards request
// bytes + query-derived Options into it.
type Config struct {
	BasePrefix string
	Importer   *importer.Importer
	// Authorizers is the same per-kind authz map the regular apply
	// pipeline consults. When set, every decoded document is
	// authorized before Upsert via importer.Options.PerObjectAuthorize;
	// a deny on any kind makes that doc fail with Status=failed
	// without aborting the rest of the batch (matches the
	// per-doc-failure pattern in pkg/registry/resource/apply.go).
	//
	// Without this map, POST /v0/import is a write-bypass for any
	// kind the Importer accepts — denied users could create / replace
	// rows by routing writes through this endpoint.
	Authorizers map[string]func(ctx context.Context, in resource.AuthorizeInput) error
}

// importInput is the HTTP input for POST /import. RawBody carries
// the multi-doc YAML stream; the query params map onto
// importer.Options.
type importInput struct {
	Namespace  string `query:"namespace" doc:"Default namespace applied to any document without metadata.namespace. Blank = v1alpha1 default."`
	Enrich     bool   `query:"enrich" doc:"Run registered scanners against each imported object."`
	WhichScans string `query:"scans" doc:"Comma-separated Scanner.Name() values to run. Empty = all supporting scanners."`
	DryRun     bool   `query:"dryRun" doc:"Validate + enrich but don't persist. Scanner side-effects still fire."`
	ScannedBy  string `query:"scannedBy" doc:"Provenance label written to enrichment_findings.scanned_by. Default 'importer-http'."`

	RawBody []byte `contentType:"application/yaml" doc:"Multi-document YAML stream of v1alpha1 resources."`
}

type importOutput struct {
	Body struct {
		Results []importer.ImportResult `json:"results"`
	}
}

// Register wires POST {BasePrefix}/import.
//
// Mirrors the apply endpoint's body + per-doc-results semantics but
// runs through the full Importer pipeline so scanner-produced
// annotations, labels, and findings land alongside the Upsert.
//
// Caller is responsible for not invoking Register unless cfg.Importer
// is wired — the router gates on that already.
func Register(api huma.API, cfg Config) {
	huma.Register(api, huma.Operation{
		OperationID: "import-batch",
		Method:      http.MethodPost,
		Path:        cfg.BasePrefix + "/import",
		Summary:     "Import v1alpha1 resources (validate, optionally enrich, upsert)",
	}, func(ctx context.Context, in *importInput) (*importOutput, error) {
		opts := importer.Options{
			Namespace: in.Namespace,
			Enrich:    in.Enrich,
			DryRun:    in.DryRun,
			ScannedBy: firstNonEmpty(in.ScannedBy, "importer-http"),
		}
		if s := strings.TrimSpace(in.WhichScans); s != "" {
			for name := range strings.SplitSeq(s, ",") {
				name = strings.TrimSpace(name)
				if name != "" {
					opts.WhichScans = append(opts.WhichScans, name)
				}
			}
		}
		if len(cfg.Authorizers) > 0 {
			authorizers := cfg.Authorizers
			opts.PerObjectAuthorize = func(ctx context.Context, obj v1alpha1.Object) error {
				kind := obj.GetKind()
				authz, ok := authorizers[kind]
				// Defense-in-depth: when the caller has wired any
				// Authorizers, a kind without an entry must DENY
				// rather than silently allow. The enterprise H2
				// boot guard already ensures every OSS BuiltinKinds
				// entry has an authorizer, so this only fires for
				// downstream kinds the operator added without
				// updating the import config — fail closed there.
				if !ok || authz == nil {
					return huma.Error403Forbidden(fmt.Sprintf("import: no authorizer wired for kind %q", kind))
				}
				meta := obj.GetMetadata()
				return authz(ctx, resource.AuthorizeInput{
					Verb: "apply", Kind: kind,
					Namespace: meta.Namespace, Name: meta.Name, Version: meta.Version,
					Object: obj,
				})
			}
		}

		out := &importOutput{}
		out.Body.Results = cfg.Importer.ImportBytes(ctx, "", in.RawBody, opts)
		return out, nil
	})
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
