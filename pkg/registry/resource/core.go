package resource

import (
	"context"
	"errors"

	arv0 "github.com/agentregistry-dev/agentregistry/pkg/api/v0"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
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
	Admission         types.Admission
	Source            string
	Prepare           func(ctx context.Context, obj v1alpha1.Object) error
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
	stageAdmission  applyStage = "admission"
	stagePrepare    applyStage = "prepare"
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
// already-decoded, metadata-stamped object:
//
//	canonicalize metadata → authorize → validate → resolve refs →
//	validate registries → prepare → admission
//
// The admission implementation owns the final write result. The OSS default
// ProductionAdmission maps dry-runs to ApplyStatusDryRun and real writes to
// Store.Upsert + PostUpsert. Returns a stage-tagged applyError on failure.
func applyCore(
	ctx context.Context,
	store *v1alpha1store.Store,
	obj v1alpha1.Object,
	opts applyOpts,
	dryRun bool,
) (types.AdmissionResult, *applyError) {
	meta := obj.GetMetadata()
	kind := obj.GetKind()

	if meta.UID != "" {
		meta.UID = ""
		obj.SetMetadata(*meta)
	}

	if v1alpha1.IsTaggedArtifactKind(kind) && meta.Tag == "" {
		meta.Tag = v1alpha1store.DefaultTag()
		obj.SetMetadata(*meta)
	}

	if opts.Authorize != nil {
		if err := opts.Authorize(ctx, AuthorizeInput{
			Verb: "apply", Kind: kind,
			Namespace: meta.Namespace, Name: meta.Name, Tag: meta.Tag,
			Object: obj,
		}); err != nil {
			return types.AdmissionResult{}, &applyError{Stage: stageAuth, Err: err}
		}
	}

	if err := v1alpha1.ValidateObject(obj); err != nil {
		return types.AdmissionResult{}, &applyError{Stage: stageValidation, Err: err}
	}
	if err := v1alpha1.ResolveObjectRefs(ctx, obj, opts.Resolver); err != nil {
		return types.AdmissionResult{}, &applyError{Stage: stageRefs, Err: err}
	}
	if err := v1alpha1.ValidateObjectRegistries(ctx, obj, opts.RegistryValidator); err != nil {
		return types.AdmissionResult{}, &applyError{Stage: stageRegistries, Err: err}
	}

	if opts.Prepare != nil {
		if err := opts.Prepare(ctx, obj); err != nil {
			return types.AdmissionResult{}, &applyError{Stage: stagePrepare, Err: err}
		}
	}

	source := opts.Source
	if source == "" {
		source = types.AdmissionSourceApply
	}
	admission := opts.Admission
	if admission == nil {
		admission = ProductionAdmission
	}
	result, err := admission(ctx, types.AdmissionInput{
		Source:            source,
		Verb:              "apply",
		DryRun:            dryRun,
		Kind:              kind,
		Namespace:         meta.Namespace,
		Name:              meta.Name,
		Tag:               meta.Tag,
		Object:            obj,
		Store:             store,
		PostUpsert:        opts.PostUpsert,
		InitialFinalizers: opts.InitialFinalizers,
	})
	if err != nil {
		if ae, ok := err.(*applyError); ok {
			return types.AdmissionResult{}, ae
		}
		return types.AdmissionResult{}, &applyError{Stage: stageAdmission, Err: err}
	}
	return result, nil
}

// ProductionAdmission is the OSS admission implementation: dry-runs stop after
// validation, and real writes upsert the object into the production store and
// run the per-kind post-upsert hook.
func ProductionAdmission(ctx context.Context, in types.AdmissionInput) (types.AdmissionResult, error) {
	if in.DryRun {
		return types.AdmissionResult{Status: arv0.ApplyStatusDryRun, Tag: in.Tag}, nil
	}
	store, ok := in.Store.(*v1alpha1store.Store)
	if !ok || store == nil {
		return types.AdmissionResult{}, errors.New("production store is required")
	}

	upsertOpts := v1alpha1store.UpsertOpts{}
	if in.InitialFinalizers != nil {
		upsertOpts.InitialFinalizers = in.InitialFinalizers(in.Object)
	}
	up, err := store.Upsert(ctx, in.Object, upsertOpts)
	if err != nil {
		return types.AdmissionResult{}, &applyError{
			Stage:       stageUpsert,
			Err:         err,
			Terminating: errors.Is(err, v1alpha1store.ErrTerminating),
		}
	}

	if in.PostUpsert != nil {
		meta := in.Object.GetMetadata()
		meta.Generation = up.Generation
		meta.UID = up.UID
		in.Object.SetMetadata(*meta)
		if err := in.PostUpsert(ctx, in.Object); err != nil {
			return types.AdmissionResult{}, &applyError{Stage: stagePostUpsert, Err: err}
		}
	}

	return types.AdmissionResult{
		Status:     applyStatusFromUpsert(up.Outcome),
		Tag:        up.Tag,
		Generation: up.Generation,
	}, nil
}

func applyStatusFromUpsert(outcome v1alpha1store.UpsertOutcome) string {
	switch outcome {
	case v1alpha1store.UpsertCreated:
		return arv0.ApplyStatusCreated
	case v1alpha1store.UpsertReplaced:
		return arv0.ApplyStatusConfigured
	case v1alpha1store.UpsertNoOp:
		return arv0.ApplyStatusUnchanged
	default:
		return arv0.ApplyStatusUnchanged
	}
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
	kind, namespace, name, tag string,
	opts deleteOpts,
) *applyError {
	if opts.Authorize != nil {
		if err := opts.Authorize(ctx, AuthorizeInput{
			Verb: "delete", Kind: kind,
			Namespace: namespace, Name: name, Tag: tag,
			Object: opts.PreDeleteObject,
		}); err != nil {
			return &applyError{Stage: stageAuth, Err: err}
		}
	}

	if err := store.Delete(ctx, namespace, name, tag); err != nil {
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
