package resource

import (
	"context"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	arv0 "github.com/agentregistry-dev/agentregistry/pkg/api/v0"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
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
	// per-kind authz wired through crud.PerKindHooks. Missing keys
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
	// (e.g. Deployment → Coordinator.Apply → platform adapter)
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

// applyOne runs a single document through the shared apply pipeline.
// Never errors; encodes any failure into the returned ApplyResult.
func applyOne(ctx context.Context, cfg ApplyConfig, obj v1alpha1.Object, dryRun bool) arv0.ApplyResult {
	store, meta, ae := resolveBatchTarget(cfg, obj, "apply")
	res := arv0.ApplyResult{
		APIVersion: obj.GetAPIVersion(),
		Kind:       obj.GetKind(),
		Namespace:  meta.Namespace,
		Name:       meta.Name,
		Version:    meta.Version,
	}
	if ae != nil {
		return failResult(res, ae)
	}

	up, ae := applyCore(ctx, store, obj, applyOpts{
		Authorize:         batchAuthorize(cfg, obj.GetKind()),
		Resolver:          cfg.Resolver,
		RegistryValidator: cfg.RegistryValidator,
		PostUpsert:        cfg.PostUpserts[obj.GetKind()],
	}, dryRun)
	if ae != nil {
		return failResult(res, ae)
	}

	if dryRun {
		res.Status = arv0.ApplyStatusDryRun
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
	return res
}

// deleteOne runs a single document through Authorize + Store.Delete +
// PostDelete. No validation: deleting a row should not require its spec
// to validate. The PostDelete hook receives the decoded body verbatim
// — batch callers expecting hook input matching the persisted row
// should re-apply before deleting.
func deleteOne(ctx context.Context, cfg ApplyConfig, obj v1alpha1.Object, dryRun bool) arv0.ApplyResult {
	store, meta, ae := resolveBatchTarget(cfg, obj, "delete")
	res := arv0.ApplyResult{
		APIVersion: obj.GetAPIVersion(),
		Kind:       obj.GetKind(),
		Namespace:  meta.Namespace,
		Name:       meta.Name,
		Version:    meta.Version,
	}
	if ae != nil {
		return failResult(res, ae)
	}

	if dryRun {
		res.Status = arv0.ApplyStatusDryRun
		return res
	}

	dopts := deleteOpts{
		Authorize:       batchAuthorize(cfg, obj.GetKind()),
		PostDelete:      cfg.PostDeletes[obj.GetKind()],
		PreDeleteObject: obj,
	}
	if ae := deleteCore(ctx, store, obj.GetKind(), meta.Namespace, meta.Name, meta.Version, dopts); ae != nil {
		return failResult(res, ae)
	}
	res.Status = arv0.ApplyStatusDeleted
	return res
}

// resolveBatchTarget looks up the per-kind store and applies the
// "default to default-namespace" + fail-closed authz-map invariants.
// Returns a non-nil applyError on a missing kind / missing authorizer
// so the caller can short-circuit the document. The returned meta is
// the namespace-defaulted view (caller must SetMetadata if needed —
// applyCore re-reads metadata after authorize so the defaulting is
// enough as long as we mutate the obj here too).
func resolveBatchTarget(cfg ApplyConfig, obj v1alpha1.Object, verb string) (*v1alpha1store.Store, v1alpha1.ObjectMeta, *applyError) {
	kind := obj.GetKind()
	meta := obj.GetMetadata()

	store, ok := cfg.Stores[kind]
	if !ok || store == nil {
		return nil, *meta, &applyError{
			Stage: applyStage(verb),
			Err:   fmt.Errorf("unknown or unconfigured kind %q", kind),
		}
	}

	// Default namespace before authorize/applyCore see it. The handler.go
	// PUT path stamps namespace from the URL; the batch path has only the
	// YAML body to look at, so default explicitly here.
	if meta.Namespace == "" {
		meta.Namespace = v1alpha1.DefaultNamespace
		obj.SetMetadata(*meta)
	}

	// Defense-in-depth: when any Authorizers are wired, a kind without
	// an entry must DENY rather than silently allow. The enterprise H2
	// boot guard already ensures every OSS BuiltinKinds entry has an
	// authorizer when authz is enabled, so this only fires for downstream
	// kinds the operator added without updating PerKindHooks — fail
	// closed there. Mirrors the same contract on the import handler.
	if len(cfg.Authorizers) > 0 {
		if authz, ok := cfg.Authorizers[kind]; !ok || authz == nil {
			return nil, *meta, &applyError{
				Stage: stageAuth,
				Err:   fmt.Errorf("forbidden: no authorizer wired for kind %q", kind),
			}
		}
	}

	return store, *meta, nil
}

// batchAuthorize returns the per-kind authz callback wrapped to match
// applyCore's Authorize signature. Returns nil when no authorizers are
// wired (the OSS-default permissive path).
func batchAuthorize(cfg ApplyConfig, kind string) func(ctx context.Context, in AuthorizeInput) error {
	if len(cfg.Authorizers) == 0 {
		return nil
	}
	return cfg.Authorizers[kind]
}

// failResult populates the ApplyResult with the stage-tagged error
// surface batch callers expect (Status="failed", Error="<stage>: <err>"
// or a stage-specific friendlier message).
func failResult(res arv0.ApplyResult, ae *applyError) arv0.ApplyResult {
	res.Status = arv0.ApplyStatusFailed
	switch ae.Stage {
	case stageAuth:
		res.Error = "forbidden: " + ae.Err.Error()
	case stageUpsert:
		if ae.Terminating {
			res.Error = fmt.Sprintf("object %s/%s/%s is terminating; delete + re-apply once GC purges the row",
				res.Namespace, res.Name, res.Version)
		} else {
			res.Error = "upsert: " + ae.Err.Error()
		}
	case stageDelete:
		if ae.NotFound {
			res.Error = fmt.Sprintf("not found: %s/%s/%s", res.Namespace, res.Name, res.Version)
		} else {
			res.Error = "delete: " + ae.Err.Error()
		}
	default:
		res.Error = ae.Error()
	}
	return res
}
