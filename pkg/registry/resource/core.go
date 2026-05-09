package resource

import (
	"context"
	"errors"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

// applyOpts threads the per-kind dependencies into the apply pipeline.
// Every field is optional; nil ⇒ skip that stage. The single-resource
// PUT handler and the multi-doc batch endpoint resolve these from
// different sources (Config vs ApplyConfig + per-kind maps) and pass
// the resolved values in.
type applyOpts struct {
	Authorize         func(ctx context.Context, in AuthorizeInput) error
	Resolver          v1alpha1.ResolverFunc
	RegistryValidator v1alpha1.RegistryValidatorFunc
	PostUpsert        func(ctx context.Context, obj v1alpha1.Object) error
	InitialFinalizers func(obj v1alpha1.Object) []string
}

// upsertResult is the outcome of a successful applyCore call.
type upsertResult struct {
	// Outcome categorises what the underlying Store.Upsert did. Callers
	// map this onto their wire status (ApplyStatusCreated, etc.).
	Outcome v1alpha1store.UpsertOutcome
	// Tag is the content tag after apply for tagged artifact stores.
	Tag string
	// Generation is the internal row generation after apply.
	Generation int64
	// UID is the server-managed row identity after production upsert.
	UID string
}

// applyStage tags which step of the pipeline produced an error so
// callers can shape their error response (huma 4xx vs ApplyResult.Error)
// without re-classifying the underlying err.
type applyStage string

const (
	stageAuth       applyStage = "auth"
	stageValidation applyStage = "validation"
	stageRefs       applyStage = "refs"
	stageRegistries applyStage = "registries"
	stageMarshal    applyStage = "marshal"
	stageUpsert     applyStage = "upsert"
	stagePostUpsert applyStage = "post-upsert"
	stageDelete     applyStage = "delete"
	stagePostDelete applyStage = "post-delete"
)

// applyError is the typed error applyCore + deleteCore return.
// Stage drives caller-side response shaping; Terminating distinguishes
// the soft-delete-in-progress case from generic upsert failures so
// callers can map it to 409 instead of 500. NotFound mirrors the same
// for delete-against-missing-row.
type applyError struct {
	Stage       applyStage
	Err         error
	Terminating bool
	NotFound    bool
}

func (e *applyError) Error() string {
	return string(e.Stage) + ": " + e.Err.Error()
}

// applyCore runs the shared upsert pipeline on a single
// already-decoded, identity-stamped object:
//
//	authorize → validate → resolve refs → validate registries →
//	marshal spec → Store.Upsert → PostUpsert
//
// dryRun=true skips Upsert + PostUpsert; everything else still runs so
// clients get the same 400-class error surface they would on a real
// apply. Returns a stage-tagged applyError on failure; nil otherwise.
func applyCore(
	ctx context.Context,
	store *v1alpha1store.Store,
	obj v1alpha1.Object,
	opts applyOpts,
	dryRun bool,
) (upsertResult, *applyError) {
	meta := obj.GetMetadata()
	kind := obj.GetKind()

	if meta.UID != "" {
		meta.UID = ""
		obj.SetMetadata(*meta)
	}

	if opts.Authorize != nil {
		if err := opts.Authorize(ctx, AuthorizeInput{
			Verb: "apply", Kind: kind,
			Namespace: meta.Namespace, Name: meta.Name, Tag: meta.Tag,
			Object: obj,
		}); err != nil {
			return upsertResult{}, &applyError{Stage: stageAuth, Err: err}
		}
	}

	if err := v1alpha1.ValidateObject(obj); err != nil {
		return upsertResult{}, &applyError{Stage: stageValidation, Err: err}
	}
	if err := v1alpha1.ResolveObjectRefs(ctx, obj, opts.Resolver); err != nil {
		return upsertResult{}, &applyError{Stage: stageRefs, Err: err}
	}
	if err := v1alpha1.ValidateObjectRegistries(ctx, obj, opts.RegistryValidator); err != nil {
		return upsertResult{}, &applyError{Stage: stageRegistries, Err: err}
	}

	if v1alpha1.IsTaggedArtifactKind(kind) && meta.Tag == "" {
		meta.Tag = v1alpha1store.DefaultTag()
		obj.SetMetadata(*meta)
	}

	if dryRun {
		return upsertResult{}, nil
	}

	upsertOpts := v1alpha1store.UpsertOpts{}
	if opts.InitialFinalizers != nil {
		upsertOpts.InitialFinalizers = opts.InitialFinalizers(obj)
	}
	up, err := store.Upsert(ctx, obj, upsertOpts)
	if err != nil {
		return upsertResult{}, &applyError{
			Stage:       stageUpsert,
			Err:         err,
			Terminating: errors.Is(err, v1alpha1store.ErrTerminating),
		}
	}
	res := upsertResult{
		Outcome:    up.Outcome,
		Tag:        up.Tag,
		Generation: up.Generation,
		UID:        up.UID,
	}

	if opts.PostUpsert != nil {
		meta.Generation = up.Generation
		meta.UID = up.UID
		obj.SetMetadata(*meta)
		if err := opts.PostUpsert(ctx, obj); err != nil {
			return res, &applyError{Stage: stagePostUpsert, Err: err}
		}
	}
	return res, nil
}

// deleteOpts threads the per-kind dependencies into deleteCore. As with
// applyOpts, every field is optional. PreDeleteObject is the object
// passed to PostDelete; callers fill it from a fresh Store.Get
// (handler.go DELETE) or from the decoded YAML body (apply.go batch
// delete). When PostDelete is nil, PreDeleteObject is unused.
type deleteOpts struct {
	Authorize       func(ctx context.Context, in AuthorizeInput) error
	PostDelete      func(ctx context.Context, obj v1alpha1.Object) error
	PreDeleteObject v1alpha1.Object
}

// deleteCore runs Authorize → Store.Delete → PostDelete for a single resource.
// Validation is intentionally skipped — deleting a row should not require its
// spec to validate.
//
// Returns NotFound=true on the missing-row case so callers can map it
// to 404 (single PUT) or "not found" Result (batch).
func deleteCore(
	ctx context.Context,
	store *v1alpha1store.Store,
	kind, namespace, name, identity string,
	opts deleteOpts,
) *applyError {
	if opts.Authorize != nil {
		if err := opts.Authorize(ctx, AuthorizeInput{
			Verb: "delete", Kind: kind,
			Namespace: namespace, Name: name, Tag: identity,
			Object: opts.PreDeleteObject,
		}); err != nil {
			return &applyError{Stage: stageAuth, Err: err}
		}
	}

	if err := store.Delete(ctx, namespace, name, identity); err != nil {
		return &applyError{
			Stage:    stageDelete,
			Err:      err,
			NotFound: errors.Is(err, pkgdb.ErrNotFound),
		}
	}

	if opts.PostDelete != nil && opts.PreDeleteObject != nil {
		if err := opts.PostDelete(ctx, opts.PreDeleteObject); err != nil {
			return &applyError{Stage: stagePostDelete, Err: err}
		}
	}
	return nil
}
