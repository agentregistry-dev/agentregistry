package builtins

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/agentregistry-dev/agentregistry/pkg/importer"
)

// ImportConfig wires POST {BasePrefix}/import. Importer is the
// pre-constructed importer.Importer (scanners + findings store +
// resolver injected at server boot); the handler forwards request
// bytes + query-derived Options into it.
type ImportConfig struct {
	BasePrefix string
	Importer   *importer.Importer
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

// RegisterImport wires POST {BasePrefix}/import.
//
// Mirrors the apply endpoint's body + per-doc-results semantics but
// runs through the full Importer pipeline so scanner-produced
// annotations, labels, and findings land alongside the Upsert.
//
// No-ops (returns without registering) when cfg.Importer is nil —
// servers without the v1alpha1 Stores wired also skip the Importer.
func RegisterImport(api huma.API, cfg ImportConfig) {
	if cfg.Importer == nil {
		return
	}
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
