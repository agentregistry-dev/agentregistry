package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

// NewResolver returns a v1alpha1.ResolverFunc that dispatches
// cross-kind ResourceRef existence checks against the supplied
// Stores map. The router wires one into its apply handler.
//
// Dangling references return v1alpha1.ErrDanglingRef so callers can
// distinguish "row missing" from "database unavailable"; unknown
// kinds return wrapped v1alpha1.ErrInvalidRef.
func NewResolver(stores map[string]*v1alpha1store.Store) v1alpha1.ResolverFunc {
	return func(ctx context.Context, ref v1alpha1.ResourceRef) error {
		store, ok := stores[ref.Kind]
		if !ok {
			return fmt.Errorf("%w: unknown kind %q", v1alpha1.ErrInvalidRef, ref.Kind)
		}
		_, err := store.GetByRef(ctx, ref.Namespace, ref.Name, ref.Tag)
		if err != nil {
			if errors.Is(err, pkgdb.ErrNotFound) {
				return v1alpha1.ErrDanglingRef
			}
			return err
		}
		return nil
	}
}

// NewGetter returns a v1alpha1.GetterFunc that dispatches a
// cross-kind ResourceRef fetch against the supplied Stores map and
// decodes the RawObject into its typed envelope via v1alpha1.Default.
// Consumers: reconcilers / runtime adapters that need the referenced
// object's Spec (not just an existence check).
//
// Dangling references return v1alpha1.ErrDanglingRef; unknown kinds
// return wrapped v1alpha1.ErrInvalidRef.
func NewGetter(stores map[string]*v1alpha1store.Store) v1alpha1.GetterFunc {
	return func(ctx context.Context, ref v1alpha1.ResourceRef) (v1alpha1.Object, error) {
		store, ok := stores[ref.Kind]
		if !ok {
			return nil, fmt.Errorf("%w: unknown kind %q", v1alpha1.ErrInvalidRef, ref.Kind)
		}
		raw, err := store.GetByRef(ctx, ref.Namespace, ref.Name, ref.Tag)
		if err != nil {
			if errors.Is(err, pkgdb.ErrNotFound) {
				return nil, v1alpha1.ErrDanglingRef
			}
			return nil, err
		}
		_, newObj, ok := v1alpha1.Default.Lookup(ref.Kind)
		if !ok {
			return nil, fmt.Errorf("%w: unknown kind %q in scheme", v1alpha1.ErrInvalidRef, ref.Kind)
		}
		obj, ok := newObj().(v1alpha1.Object)
		if !ok {
			return nil, fmt.Errorf("scheme constructor for %q did not return v1alpha1.Object", ref.Kind)
		}
		// scanRow leaves RawObject.TypeMeta zero (apiVersion/kind aren't
		// persisted as columns — they're implicit per table), so pin them
		// from the ref + scheme defaults. Adapters rely on GetKind() to
		// dispatch.
		obj.SetTypeMeta(v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: ref.Kind})
		obj.SetMetadata(raw.Metadata)
		if len(raw.Status) > 0 {
			if err := obj.UnmarshalStatus(raw.Status); err != nil {
				return nil, fmt.Errorf("decode %s status: %w", ref.Kind, err)
			}
		}
		if len(raw.Spec) > 0 {
			if err := obj.UnmarshalSpec(raw.Spec); err != nil {
				return nil, fmt.Errorf("decode %s spec: %w", ref.Kind, err)
			}
		}
		return obj, nil
	}
}

// NewKagentDeploymentFinder returns the Kagent lookup for a managed Deployment
// targeting a resource on a particular runtime. Missing Deployments return false.
func NewKagentDeploymentFinder(stores map[string]*v1alpha1store.Store) func(
	context.Context,
	v1alpha1.ResourceRef,
	v1alpha1.ResourceRef,
) (*v1alpha1.Deployment, bool, error) {
	return func(
		ctx context.Context,
		targetRef v1alpha1.ResourceRef,
		runtimeRef v1alpha1.ResourceRef,
	) (*v1alpha1.Deployment, bool, error) {
		store, ok := stores[v1alpha1.KindDeployment]
		if !ok {
			return nil, false, fmt.Errorf("deployment store is not configured")
		}
		rows, _, err := store.List(ctx, v1alpha1store.ListOpts{
			Limit: 1,
			ExtraWhere: "spec->'targetRef'->>'kind' = $1 AND " +
				"spec->'targetRef'->>'name' = $2 AND " +
				"COALESCE(NULLIF(spec->'targetRef'->>'namespace', ''), namespace) = $3 AND " +
				"COALESCE(NULLIF(spec->'targetRef'->>'tag', ''), 'latest') = " +
				"COALESCE(NULLIF($4, ''), 'latest') AND " +
				"spec->'runtimeRef'->>'name' = $5 AND " +
				"COALESCE(NULLIF(spec->'runtimeRef'->>'namespace', ''), namespace) = $6 AND " +
				"COALESCE(annotations->>'agentregistry.solo.io/origin', 'managed') <> 'discovered'",
			ExtraArgs: []any{
				targetRef.Kind,
				targetRef.Name,
				targetRef.Namespace,
				targetRef.Tag,
				runtimeRef.Name,
				runtimeRef.Namespace,
			},
		})
		if err != nil {
			return nil, false, fmt.Errorf(
				"find Deployment for %s %s/%s: %w",
				targetRef.Kind,
				targetRef.Namespace,
				targetRef.Name,
				err,
			)
		}
		if len(rows) == 0 {
			return nil, false, nil
		}
		deployment, err := v1alpha1.EnvelopeFromRaw(func() *v1alpha1.Deployment {
			return &v1alpha1.Deployment{}
		}, rows[0], v1alpha1.KindDeployment)
		if err != nil {
			return nil, false, fmt.Errorf(
				"decode Deployment %s/%s: %w",
				targetRef.Namespace,
				rows[0].Metadata.Name,
				err,
			)
		}
		return deployment, true, nil
	}
}
