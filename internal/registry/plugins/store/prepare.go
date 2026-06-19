package store

import (
	"context"
	"fmt"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// NewPluginPrepare returns the per-kind Prepare hook for Plugin resources. It
// runs after validation and before Store.Upsert: it resolves the plugin's
// origin to bundle bytes (via src), canonicalizes them, pushes the canonical
// bundle to st, and populates spec.Content + spec.Manifest IN-FLIGHT so they
// persist in the single Upsert. Because the populated fields are part of the
// spec when the store computes its content_hash, an identical re-apply that
// resolves to the same bundle is a no-op (idempotent publish).
//
// Fail-closed: if st or src is nil (no plugin storage configured), the hook
// rejects Plugin applies rather than persisting a Content-less, un-pullable
// row. The return type matches types.Prepare without importing pkg/types.
func NewPluginPrepare(src BundleSource, st Store) func(ctx context.Context, obj v1alpha1.Object) error {
	return func(ctx context.Context, obj v1alpha1.Object) error {
		p, ok := obj.(*v1alpha1.Plugin)
		if !ok {
			// The per-kind hook map keys this to KindPlugin; a mismatch means a
			// wiring bug, but no-op defensively rather than corrupting another kind.
			return nil
		}
		if st == nil || src == nil {
			return fmt.Errorf("plugin storage is not configured (set AGENT_REGISTRY_PLUGIN_REGISTRY); cannot publish plugins")
		}

		raw, err := src.Fetch(ctx, p)
		if err != nil {
			return fmt.Errorf("fetch plugin bundle: %w", err)
		}
		bundle, err := Canonicalize(raw)
		if err != nil {
			return fmt.Errorf("canonicalize plugin bundle: %w", err)
		}

		meta := p.GetMetadata()
		tag := meta.Tag
		if tag == "" {
			tag = "latest" // defensive; admission fills the default tag before Prepare
		}
		ociRef, contentHash, err := st.Push(ctx, meta.Namespace, meta.Name, tag, bundle)
		if err != nil {
			return fmt.Errorf("store plugin bundle: %w", err)
		}

		p.Spec.Content = &v1alpha1.PluginContent{ContentHash: contentHash, OCIRef: ociRef}
		p.Spec.Manifest = ParseManifest(bundle)
		return nil
	}
}
