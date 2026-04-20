package resource

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/agentregistry-dev/agentregistry/internal/registry/api/apitypes"
	"github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

// ApplyConfig is the per-server configuration for the multi-doc apply
// endpoints. Stores maps a v1alpha1 Kind to the matching database.Store.
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
	// RegistryValidator is forwarded to each decoded object's
	// ValidateRegistries. Nil skips external-registry validation.
	RegistryValidator v1alpha1.RegistryValidatorFunc
	// UniqueRemoteURLsChecker is forwarded to each decoded object's
	// ValidateUniqueRemoteURLs. Nil skips the uniqueness check.
	UniqueRemoteURLsChecker v1alpha1.UniqueRemoteURLsFunc
	// Scheme decodes the incoming YAML/JSON stream. Defaults to
	// v1alpha1.Default when nil.
	Scheme *v1alpha1.Scheme
}

// ApplyResult is a re-export of apitypes.ApplyResult so existing handler
// tests keep working without importing apitypes directly.
type ApplyResult = apitypes.ApplyResult

const (
	ApplyStatusCreated    = apitypes.ApplyStatusCreated
	ApplyStatusConfigured = apitypes.ApplyStatusConfigured
	ApplyStatusUnchanged  = apitypes.ApplyStatusUnchanged
	ApplyStatusDeleted    = apitypes.ApplyStatusDeleted
	ApplyStatusDryRun     = apitypes.ApplyStatusDryRun
	ApplyStatusFailed     = apitypes.ApplyStatusFailed
)

// applyInput receives a raw multi-doc YAML stream. RawBody keeps bytes
// intact so sigs.k8s.io/yaml (used by v1alpha1.Scheme) can split and
// decode each `---`-separated document without Huma pre-interpreting
// the body as JSON.
//
// DryRun runs validate + resolve + registries + uniqueness but does not
// mutate the store. Force is accepted for CLI compatibility and is
// currently a no-op: v1alpha1's Store.Upsert handles version/spec
// updates via generation + pickLatestVersion, so no cross-apply drift
// gate is required.
type applyInput struct {
	DryRun  bool   `query:"dryRun" doc:"Run validation and enrichment without mutating the store. Defaults to false."`
	Force   bool   `query:"force" doc:"Accepted for backwards compatibility; no-op under v1alpha1."`
	RawBody []byte `contentType:"application/yaml" doc:"Multi-document YAML stream of v1alpha1 resources."`
}

type applyOutput struct {
	Body apitypes.ApplyResultsResponse
}

// RegisterApply wires POST {BasePrefix}/apply and DELETE {BasePrefix}/apply.
//
// POST: for each document, stamps TypeMeta, validates, resolves refs
// (when Resolver is set), runs registry + uniqueness checks, and
// Upserts via the kind-matched Store.
//
// DELETE: for each document, calls Store.Delete on the (namespace,
// name, version) triple — soft-delete semantics (sets deletionTimestamp,
// finalizers own hard-deletion). Validation still runs so clients get
// the same error surface as apply.
//
// Both endpoints always return 200 with a per-document Results slice;
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
		return runApplyBatch(ctx, cfg, scheme, in, false), nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "delete-batch",
		Method:      http.MethodDelete,
		Path:        cfg.BasePrefix + "/apply",
		Summary:     "Delete v1alpha1 resources identified by a multi-doc YAML stream",
	}, func(ctx context.Context, in *applyInput) (*applyOutput, error) {
		return runApplyBatch(ctx, cfg, scheme, in, true), nil
	})
}

func runApplyBatch(ctx context.Context, cfg ApplyConfig, scheme *v1alpha1.Scheme, in *applyInput, del bool) *applyOutput {
	if in.DryRun {
		ctx = context.WithValue(ctx, dryRunKey{}, true)
	}
	out := &applyOutput{}
	docs, err := scheme.DecodeMulti(in.RawBody)
	if err != nil {
		out.Body.Results = []apitypes.ApplyResult{{
			Status: ApplyStatusFailed,
			Error:  "decode: " + err.Error(),
		}}
		return out
	}
	out.Body.Results = make([]apitypes.ApplyResult, 0, len(docs))
	for _, d := range docs {
		obj, ok := d.(v1alpha1.Object)
		if !ok {
			out.Body.Results = append(out.Body.Results, apitypes.ApplyResult{
				Status: ApplyStatusFailed,
				Error:  fmt.Sprintf("decoded value does not satisfy v1alpha1.Object: %T", d),
			})
			continue
		}
		if del {
			out.Body.Results = append(out.Body.Results, deleteOne(ctx, cfg, obj))
		} else {
			out.Body.Results = append(out.Body.Results, applyOne(ctx, cfg, obj))
		}
	}
	return out
}

// applyOne runs a single document through validation + ResolveRefs +
// Store.Upsert (skipping Upsert on DryRun). Never errors; encodes any
// failure into the returned ApplyResult.
func applyOne(ctx context.Context, cfg ApplyConfig, obj v1alpha1.Object) apitypes.ApplyResult {
	res, store, meta, ok := prepareApplyDoc(ctx, cfg, obj)
	if !ok {
		return res
	}

	dryRun, _ := ctx.Value(dryRunKey{}).(bool)
	if dryRun {
		res.Status = ApplyStatusDryRun
		return res
	}

	specJSON, err := obj.MarshalSpec()
	if err != nil {
		res.Status = ApplyStatusFailed
		res.Error = "marshal spec: " + err.Error()
		return res
	}

	upsertOpts := database.UpsertOpts{}
	if meta.Finalizers != nil {
		upsertOpts.Finalizers = meta.Finalizers
	}
	if meta.Annotations != nil {
		upsertOpts.Annotations = meta.Annotations
	}
	up, err := store.Upsert(ctx, meta.Namespace, meta.Name, meta.Version, specJSON, meta.Labels, upsertOpts)
	if err != nil {
		res.Status = ApplyStatusFailed
		res.Error = "upsert: " + err.Error()
		return res
	}

	switch {
	case up.Created:
		res.Status = ApplyStatusCreated
	case up.SpecChanged:
		res.Status = ApplyStatusConfigured
	default:
		res.Status = ApplyStatusUnchanged
	}
	res.Generation = up.Generation
	return res
}

// deleteOne runs a single document through validation (so 400-style
// errors still surface) and then Store.Delete. Missing rows return
// Status="failed" rather than a silent success.
func deleteOne(ctx context.Context, cfg ApplyConfig, obj v1alpha1.Object) apitypes.ApplyResult {
	res, store, meta, ok := prepareApplyDoc(ctx, cfg, obj)
	if !ok {
		return res
	}

	dryRun, _ := ctx.Value(dryRunKey{}).(bool)
	if dryRun {
		res.Status = ApplyStatusDryRun
		return res
	}

	if err := store.Delete(ctx, meta.Namespace, meta.Name, meta.Version); err != nil {
		res.Status = ApplyStatusFailed
		if errors.Is(err, pkgdb.ErrNotFound) {
			res.Error = fmt.Sprintf("not found: %s/%s/%s", meta.Namespace, meta.Name, meta.Version)
		} else {
			res.Error = "delete: " + err.Error()
		}
		return res
	}
	res.Status = ApplyStatusDeleted
	return res
}

// prepareApplyDoc runs the common namespace-default + validate + refs +
// registries + uniqueness pipeline. Returns (result, store, meta,
// true) on success. Returns (result, nil, nil, false) when a validation
// or lookup step failed; caller should return the result unchanged.
func prepareApplyDoc(ctx context.Context, cfg ApplyConfig, obj v1alpha1.Object) (apitypes.ApplyResult, *database.Store, *v1alpha1.ObjectMeta, bool) {
	kind := obj.GetKind()
	meta := obj.GetMetadata()

	res := apitypes.ApplyResult{
		APIVersion: obj.GetAPIVersion(),
		Kind:       kind,
		Namespace:  meta.Namespace,
		Name:       meta.Name,
		Version:    meta.Version,
	}

	store, ok := cfg.Stores[kind]
	if !ok || store == nil {
		res.Status = ApplyStatusFailed
		res.Error = fmt.Sprintf("unknown or unconfigured kind %q", kind)
		return res, nil, nil, false
	}

	if meta.Namespace == "" {
		meta.Namespace = v1alpha1.DefaultNamespace
		obj.SetMetadata(*meta)
		res.Namespace = meta.Namespace
	}

	if err := obj.Validate(); err != nil {
		res.Status = ApplyStatusFailed
		res.Error = "validation: " + err.Error()
		return res, nil, nil, false
	}
	if cfg.Resolver != nil {
		if err := obj.ResolveRefs(ctx, cfg.Resolver); err != nil {
			res.Status = ApplyStatusFailed
			res.Error = "refs: " + err.Error()
			return res, nil, nil, false
		}
	}
	if cfg.RegistryValidator != nil {
		if err := obj.ValidateRegistries(ctx, cfg.RegistryValidator); err != nil {
			res.Status = ApplyStatusFailed
			res.Error = "registries: " + err.Error()
			return res, nil, nil, false
		}
	}
	if cfg.UniqueRemoteURLsChecker != nil {
		if err := obj.ValidateUniqueRemoteURLs(ctx, cfg.UniqueRemoteURLsChecker); err != nil {
			res.Status = ApplyStatusFailed
			res.Error = "remote urls: " + err.Error()
			return res, nil, nil, false
		}
	}

	return res, store, meta, true
}

// dryRunKey threads the DryRun flag from applyInput down to applyOne
// without changing every helper's signature.
type dryRunKey struct{}
