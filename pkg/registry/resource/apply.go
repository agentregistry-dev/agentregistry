package resource

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	arv0 "github.com/agentregistry-dev/agentregistry/pkg/api/v0"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

// ApplyConfig is the per-server configuration for the multi-doc apply
// endpoints. Stores maps a v1alpha1 Kind to the matching v1alpha1store.Store.
// Resolver optionally checks cross-kind ResourceRef existence; when nil
// ResolveRefs is skipped.
type ApplyConfig struct {
	// BasePrefix is the HTTP route prefix shared with the generic resource
	// handler (e.g. "/v0"). The apply endpoint mounts at
	// "{BasePrefix}/apply".
	BasePrefix string
	// Stores maps Kind ("Agent", "MCPServer", etc.) to its Store.
	Stores map[string]*v1alpha1store.Store
	// Resolver is forwarded to each decoded object's ResolveRefs.
	Resolver v1alpha1.ResolverFunc
	// RegistryValidator is forwarded to each decoded object's
	// ValidateRegistries. Nil skips external-registry validation.
	RegistryValidator v1alpha1.RegistryValidatorFunc
	// Scheme decodes the incoming YAML/JSON stream. Defaults to
	// v1alpha1.Default when nil.
	Scheme *v1alpha1.Scheme
	// Authorizers, when non-empty, gates each decoded document on apply
	// against the same per-kind hook the generic resource handler
	// consults at PUT /v0/{plural}/{name}/{version}. Without this,
	// /v0/apply (the multi-doc batch endpoint arctl uses) bypasses the
	// per-kind authz wired through v1alpha1crud.PerKindHooks. Missing keys
	// authorize-allow (matches resource.Config.Authorize == nil).
	//
	// Each document gets its own AuthorizeInput (Verb="apply", Kind +
	// Name + Version + Namespace from the decoded metadata) so the
	// caller can deny per-resource. Errors fail the document; the rest
	// of the batch continues — same per-doc isolation the upsert path
	// already has.
	Authorizers map[string]func(ctx context.Context, in AuthorizeInput) error

	// PostUpserts mirrors resource.Config.PostUpsert per kind. Without
	// it, kinds that drive runtime reconciliation through PostUpsert
	// (e.g. Deployment → V1Alpha1Coordinator.Apply → platform adapter)
	// are silently skipped when the resource is applied via the batch
	// endpoint instead of the namespaced PUT. Per-doc errors fail the
	// individual result; the rest of the batch continues.
	PostUpserts map[string]func(ctx context.Context, obj v1alpha1.Object) error

	// PostDeletes mirrors resource.Config.PostDelete per kind. Same
	// rationale as PostUpserts — Deployment delete via batch otherwise
	// soft-deletes the row but never tears down the platform adapter
	// state.
	PostDeletes map[string]func(ctx context.Context, obj v1alpha1.Object) error
}

// applyInput receives a raw multi-doc YAML stream. RawBody keeps bytes
// intact so sigs.k8s.io/yaml (used by v1alpha1.Scheme) can split and
// decode each `---`-separated document without Huma pre-interpreting
// the body as JSON.
//
// DryRun runs validate + resolve + registries + uniqueness but does not
// mutate the store.
type applyInput struct {
	DryRun  bool   `query:"dryRun" doc:"Run validation and enrichment without mutating the store. Defaults to false."`
	RawBody []byte `contentType:"application/yaml" doc:"Multi-document YAML stream of v1alpha1 resources."`
}

type applyOutput struct {
	Body arv0.ApplyResultsResponse
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
	out := &applyOutput{}
	docs, err := scheme.DecodeMulti(in.RawBody)
	if err != nil {
		out.Body.Results = []arv0.ApplyResult{{
			Status: arv0.ApplyStatusFailed,
			Error:  "decode: " + err.Error(),
		}}
		return out
	}
	out.Body.Results = make([]arv0.ApplyResult, 0, len(docs))
	for _, d := range docs {
		obj, ok := d.(v1alpha1.Object)
		if !ok {
			out.Body.Results = append(out.Body.Results, arv0.ApplyResult{
				Status: arv0.ApplyStatusFailed,
				Error:  fmt.Sprintf("decoded value does not satisfy v1alpha1.Object: %T", d),
			})
			continue
		}
		if del {
			out.Body.Results = append(out.Body.Results, deleteOne(ctx, cfg, obj, in.DryRun))
		} else {
			out.Body.Results = append(out.Body.Results, applyOne(ctx, cfg, obj, in.DryRun))
		}
	}
	return out
}

// preparedDoc is the outcome of prepareApplyDoc: either a shortcut Result
// (populated with Status="failed" + Error) when a validation step rejected
// the document, or a Store + ObjectMeta ready for Upsert/Delete when
// validation passed. Exactly one of Ready / Result populated.
type preparedDoc struct {
	Result arv0.ApplyResult
	Ready  bool
	Store  *v1alpha1store.Store
	Meta   *v1alpha1.ObjectMeta
}

// applyOne runs a single document through validation + ResolveRefs +
// Store.Upsert (skipping Upsert on dryRun). Never errors; encodes any
// failure into the returned ApplyResult.
func applyOne(ctx context.Context, cfg ApplyConfig, obj v1alpha1.Object, dryRun bool) arv0.ApplyResult {
	pd := prepareApplyDoc(ctx, cfg, obj)
	if !pd.Ready {
		return pd.Result
	}
	res, store, meta := pd.Result, pd.Store, pd.Meta

	if dryRun {
		res.Status = arv0.ApplyStatusDryRun
		return res
	}

	specJSON, err := obj.MarshalSpec()
	if err != nil {
		res.Status = arv0.ApplyStatusFailed
		res.Error = "marshal spec: " + err.Error()
		return res
	}

	upsertOpts := v1alpha1store.UpsertOpts{Labels: meta.Labels}
	if meta.Annotations != nil {
		upsertOpts.Annotations = meta.Annotations
	}
	up, err := store.Upsert(ctx, meta.Namespace, meta.Name, meta.Version, specJSON, upsertOpts)
	if err != nil {
		res.Status = arv0.ApplyStatusFailed
		if errors.Is(err, v1alpha1store.ErrTerminating) {
			res.Error = fmt.Sprintf("object %s/%s/%s is terminating; delete + re-apply once GC purges the row",
				meta.Namespace, meta.Name, meta.Version)
		} else {
			res.Error = "upsert: " + err.Error()
		}
		return res
	}

	switch {
	case up.Created:
		res.Status = arv0.ApplyStatusCreated
	case up.SpecChanged:
		res.Status = arv0.ApplyStatusConfigured
	default:
		res.Status = arv0.ApplyStatusUnchanged
	}
	res.Generation = up.Generation

	// Post-upsert hook (per kind) — same dispatch the per-kind
	// handler.go runs after PUT. This is what drives the platform
	// adapter for Deployment kind; without it a Deployment applied
	// via batch creates the row but never reconciles to AWS / GCP /
	// kagent runtime state.
	if hook := cfg.PostUpserts[obj.GetKind()]; hook != nil {
		if err := hook(ctx, obj); err != nil {
			res.Status = arv0.ApplyStatusFailed
			res.Error = "post-upsert: " + err.Error()
		}
	}
	return res
}

// deleteOne runs a single document through validation (so 400-style
// errors still surface) and then Store.Delete. Missing rows return
// Status="failed" rather than a silent success.
func deleteOne(ctx context.Context, cfg ApplyConfig, obj v1alpha1.Object, dryRun bool) arv0.ApplyResult {
	pd := prepareApplyDoc(ctx, cfg, obj)
	if !pd.Ready {
		return pd.Result
	}
	res, store, meta := pd.Result, pd.Store, pd.Meta

	if dryRun {
		res.Status = arv0.ApplyStatusDryRun
		return res
	}

	if err := store.Delete(ctx, meta.Namespace, meta.Name, meta.Version); err != nil {
		res.Status = arv0.ApplyStatusFailed
		if errors.Is(err, pkgdb.ErrNotFound) {
			res.Error = fmt.Sprintf("not found: %s/%s/%s", meta.Namespace, meta.Name, meta.Version)
		} else {
			res.Error = "delete: " + err.Error()
		}
		return res
	}
	res.Status = arv0.ApplyStatusDeleted

	// Post-delete hook (per kind) — same dispatch the per-kind
	// handler.go runs after DELETE. Mirrors PostUpserts above; without
	// it a Deployment deleted via batch soft-deletes the row but never
	// tears down adapter state.
	if hook := cfg.PostDeletes[obj.GetKind()]; hook != nil {
		if err := hook(ctx, obj); err != nil {
			res.Status = arv0.ApplyStatusFailed
			res.Error = "post-delete: " + err.Error()
		}
	}
	return res
}

// prepareApplyDoc runs the common namespace-default + validate + refs +
// registries + uniqueness pipeline. On success returns a preparedDoc
// with Ready=true + Store + Meta. On failure returns Ready=false and
// Result populated with Status=failed + Error; caller returns it
// unchanged.
func prepareApplyDoc(ctx context.Context, cfg ApplyConfig, obj v1alpha1.Object) preparedDoc {
	kind := obj.GetKind()
	meta := obj.GetMetadata()

	pd := preparedDoc{
		Result: arv0.ApplyResult{
			APIVersion: obj.GetAPIVersion(),
			Kind:       kind,
			Namespace:  meta.Namespace,
			Name:       meta.Name,
			Version:    meta.Version,
		},
	}

	store, ok := cfg.Stores[kind]
	if !ok || store == nil {
		pd.Result.Status = arv0.ApplyStatusFailed
		pd.Result.Error = fmt.Sprintf("unknown or unconfigured kind %q", kind)
		return pd
	}

	if meta.Namespace == "" {
		meta.Namespace = v1alpha1.DefaultNamespace
		obj.SetMetadata(*meta)
		pd.Result.Namespace = meta.Namespace
	}

	// Per-kind authz fires before validation so a denied apply doesn't
	// leak validation errors back to the caller. Same semantics as the
	// resource handler's PUT path.
	//
	// Defense-in-depth: when any Authorizers are wired, a kind without
	// an entry must DENY rather than silently allow. The enterprise H2
	// boot guard already ensures every OSS BuiltinKinds entry has an
	// authorizer when authz is enabled, so this only fires for
	// downstream kinds the operator added without updating PerKindHooks
	// — fail closed there. Mirrors the same fail-closed contract on the
	// import handler (`f8682fb`).
	if len(cfg.Authorizers) > 0 {
		authz, ok := cfg.Authorizers[kind]
		if !ok || authz == nil {
			pd.Result.Status = arv0.ApplyStatusFailed
			pd.Result.Error = fmt.Sprintf("forbidden: no authorizer wired for kind %q", kind)
			return pd
		}
		if err := authz(ctx, AuthorizeInput{
			Verb: "apply", Kind: kind,
			Namespace: meta.Namespace, Name: meta.Name, Version: meta.Version,
			Object: obj,
		}); err != nil {
			pd.Result.Status = arv0.ApplyStatusFailed
			pd.Result.Error = "forbidden: " + err.Error()
			return pd
		}
	}

	if err := v1alpha1.ValidateObject(obj); err != nil {
		pd.Result.Status = arv0.ApplyStatusFailed
		pd.Result.Error = "validation: " + err.Error()
		return pd
	}
	if err := v1alpha1.ResolveObjectRefs(ctx, obj, cfg.Resolver); err != nil {
		pd.Result.Status = arv0.ApplyStatusFailed
		pd.Result.Error = "refs: " + err.Error()
		return pd
	}
	if err := v1alpha1.ValidateObjectRegistries(ctx, obj, cfg.RegistryValidator); err != nil {
		pd.Result.Status = arv0.ApplyStatusFailed
		pd.Result.Error = "registries: " + err.Error()
		return pd
	}
	pd.Ready = true
	pd.Store = store
	pd.Meta = meta
	return pd
}
