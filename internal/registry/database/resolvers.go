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
// Stores map. Consumers: the router wires one into its apply
// handler; the Importer consumes one during per-object ResolveRefs.
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
		var err error
		if ref.Version == "" {
			_, err = store.GetLatest(ctx, ref.Namespace, ref.Name)
		} else {
			_, err = store.Get(ctx, ref.Namespace, ref.Name, ref.Version)
		}
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
// Consumers: reconcilers / platform adapters that need the referenced
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
		var (
			raw *v1alpha1.RawObject
			err error
		)
		if ref.Version == "" {
			raw, err = store.GetLatest(ctx, ref.Namespace, ref.Name)
		} else {
			raw, err = store.Get(ctx, ref.Namespace, ref.Name, ref.Version)
		}
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
