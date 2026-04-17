package resource

import (
	"context"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// ApplyConfig is the per-server configuration for the multi-doc apply
// endpoint. Stores maps a v1alpha1 Kind to the matching database.Store.
// Resolver optionally checks cross-kind ResourceRef existence; when nil
// ResolveRefs is skipped.
type ApplyConfig struct {
	// BasePrefix is the HTTP route prefix shared with the generic resource
	// handler (e.g. "/v0"). The apply endpoint mounts at
	// "{BasePrefix}/apply".
	BasePrefix string
	// Stores maps Kind ("Agent", "MCPServer", etc.) to its Store.
	Stores map[string]*database.Store
	// Resolver is forwarded to each decoded object's ResolveRefs.
	Resolver v1alpha1.ResolverFunc
	// Scheme decodes the incoming YAML/JSON stream. Defaults to
	// v1alpha1.Default when nil.
	Scheme *v1alpha1.Scheme
}

// ApplyResult describes the outcome for a single document in a
// multi-doc apply request.
type ApplyResult struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name"`
	Version    string `json:"version,omitempty"`
	// Status is one of: created, configured, unchanged, failed. Matches
	// Kubernetes-style kubectl apply output.
	Status string `json:"status"`
	// Generation is the server-managed generation after the apply. Zero
	// for failed docs.
	Generation int64 `json:"generation,omitempty"`
	// Error is the failure detail for Status=="failed".
	Error string `json:"error,omitempty"`
}

const (
	ApplyStatusCreated    = "created"
	ApplyStatusConfigured = "configured"
	ApplyStatusUnchanged  = "unchanged"
	ApplyStatusFailed     = "failed"
)

// applyInput receives a raw multi-doc YAML stream. RawBody keeps bytes
// intact so sigs.k8s.io/yaml (used by v1alpha1.Scheme) can split and
// decode each `---`-separated document without Huma pre-interpreting
// the body as JSON.
type applyInput struct {
	RawBody []byte `contentType:"application/yaml" doc:"Multi-document YAML stream of v1alpha1 resources."`
}

type applyOutput struct {
	Body struct {
		Results []ApplyResult `json:"results"`
	}
}

// RegisterApply wires POST {BasePrefix}/apply. Accepts a multi-doc YAML
// stream; decodes with cfg.Scheme; for each document: stamps TypeMeta,
// validates, resolves refs (when Resolver is set), and Upserts via the
// kind-matched Store.
//
// The endpoint always returns 200 with a per-document Results slice;
// document-level failures are surfaced as Status="failed" entries and
// do not short-circuit the batch. Callers diff Results to decide
// whether to retry.
func RegisterApply(api huma.API, cfg ApplyConfig) {
	scheme := cfg.Scheme
	if scheme == nil {
		scheme = v1alpha1.Default
	}
	huma.Register(api, huma.Operation{
		OperationID: "apply-batch",
		Method:      http.MethodPost,
		Path:        cfg.BasePrefix + "/apply",
		Summary:     "Apply a multi-doc YAML stream of v1alpha1 resources",
	}, func(ctx context.Context, in *applyInput) (*applyOutput, error) {
		docs, err := scheme.DecodeMulti(in.RawBody)
		if err != nil {
			return nil, huma.Error400BadRequest("decode: " + err.Error())
		}

		out := &applyOutput{}
		out.Body.Results = make([]ApplyResult, 0, len(docs))
		for _, d := range docs {
			obj, ok := d.(v1alpha1.Object)
			if !ok {
				out.Body.Results = append(out.Body.Results, ApplyResult{
					Status: ApplyStatusFailed,
					Error:  fmt.Sprintf("decoded value does not satisfy v1alpha1.Object: %T", d),
				})
				continue
			}
			out.Body.Results = append(out.Body.Results, applyOne(ctx, cfg, obj))
		}
		return out, nil
	})
}

// applyOne runs a single document through validation + ResolveRefs +
// Store.Upsert. Never errors; encodes any failure into the returned
// ApplyResult.
func applyOne(ctx context.Context, cfg ApplyConfig, obj v1alpha1.Object) ApplyResult {
	kind := obj.GetKind()
	meta := obj.GetMetadata()

	result := ApplyResult{
		APIVersion: obj.GetAPIVersion(),
		Kind:       kind,
		Namespace:  meta.Namespace,
		Name:       meta.Name,
		Version:    meta.Version,
	}

	store, ok := cfg.Stores[kind]
	if !ok || store == nil {
		result.Status = ApplyStatusFailed
		result.Error = fmt.Sprintf("unknown or unconfigured kind %q", kind)
		return result
	}

	if meta.Namespace == "" {
		meta.Namespace = v1alpha1.DefaultNamespace
		obj.SetMetadata(*meta)
	}

	if err := obj.Validate(); err != nil {
		result.Status = ApplyStatusFailed
		result.Error = "validation: " + err.Error()
		return result
	}
	if cfg.Resolver != nil {
		if err := obj.ResolveRefs(ctx, cfg.Resolver); err != nil {
			result.Status = ApplyStatusFailed
			result.Error = "refs: " + err.Error()
			return result
		}
	}

	specJSON, err := obj.MarshalSpec()
	if err != nil {
		result.Status = ApplyStatusFailed
		result.Error = "marshal spec: " + err.Error()
		return result
	}

	upsertOpts := database.UpsertOpts{}
	if meta.Finalizers != nil {
		upsertOpts.Finalizers = meta.Finalizers
	}
	if meta.Annotations != nil {
		upsertOpts.Annotations = meta.Annotations
	}
	res, err := store.Upsert(ctx, meta.Namespace, meta.Name, meta.Version, specJSON, meta.Labels, upsertOpts)
	if err != nil {
		result.Status = ApplyStatusFailed
		result.Error = "upsert: " + err.Error()
		return result
	}

	switch {
	case res.Created:
		result.Status = ApplyStatusCreated
	case res.SpecChanged:
		result.Status = ApplyStatusConfigured
	default:
		result.Status = ApplyStatusUnchanged
	}
	result.Generation = res.Generation
	return result
}
