package controller

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
	"istio.io/istio/pkg/kube/krt"
)

const listAllPageSize = 500

// SourceRow is a KRT source row keyed by v1alpha1 resource identity. The
// Object stays generic because projection only maintains the read model;
// derivers own any feature-specific type assertions.
type SourceRow struct {
	Key    v1alpha1store.ResourceKey
	Object v1alpha1.Object
}

// ResourceName implements krt.ResourceNamer.
func (r SourceRow) ResourceName() string {
	return sourceObjectKey(r.Key)
}

type sourceKind struct {
	Kind               string
	IncludeTerminating bool
}

// SourceIndex is the controller-owned read model for v1alpha1 source objects.
// It projects every globally registered kind that also has a Store, so adding
// a new kind only requires registering that kind with the API scheme/store
// setup instead of adding controller switch cases.
type SourceIndex struct {
	stores map[string]*v1alpha1store.Store
	kinds  map[string]sourceKind

	items krt.StaticCollection[SourceRow]
}

func NewSourceIndex(stores map[string]*v1alpha1store.Store) *SourceIndex {
	kinds := make(map[string]sourceKind, len(stores))
	for kind := range stores {
		if _, ok := newRegisteredObject(kind); !ok {
			continue
		}
		var projection v1alpha1.ProjectionPolicy
		if descriptor, ok := v1alpha1.BuiltinKindDescriptor(kind); ok {
			projection = descriptor.Projection
		}
		kinds[kind] = sourceKind{
			Kind:               kind,
			IncludeTerminating: projection.IncludeTerminating,
		}
	}
	return &SourceIndex{
		stores: stores,
		kinds:  kinds,
		items:  krt.NewStaticCollection[SourceRow](nil, nil, krt.WithName("agentregistry-sources")),
	}
}

// Refresh rebuilds every source collection from canonical store tables.
func (s *SourceIndex) Refresh(ctx context.Context) error {
	if s == nil {
		return errors.New("controller sources: sources are required")
	}
	var rows []SourceRow
	for _, kind := range s.orderedKinds() {
		kindRows, err := s.listAll(ctx, s.kinds[kind])
		if err != nil {
			return err
		}
		rows = append(rows, kindRows...)
	}
	s.items.Reset(rows)
	return nil
}

// ApplyEvent incrementally refreshes one projected source row.
func (s *SourceIndex) ApplyEvent(ctx context.Context, event v1alpha1store.ControlPlaneEvent) error {
	if s == nil {
		return errors.New("controller sources: sources are required")
	}
	kind, ok := s.kinds[event.Key.Kind]
	if !ok {
		return nil
	}
	return s.applySourceEvent(ctx, kind, event)
}

func (s *SourceIndex) DeploymentList() []SourceRow {
	if s == nil {
		return nil
	}
	return s.ListKind(v1alpha1.KindDeployment)
}

func (s *SourceIndex) TargetExists(deployment *v1alpha1.Deployment) bool {
	if s == nil || deployment == nil {
		return false
	}
	return s.ResourceExists(deployment.Spec.TargetRef, deployment.Metadata.NamespaceOrDefault())
}

func (s *SourceIndex) RuntimeExists(deployment *v1alpha1.Deployment) bool {
	if s == nil || deployment == nil {
		return false
	}
	ref := deployment.Spec.RuntimeRef
	ref.Kind = v1alpha1.KindRuntime
	return s.ResourceExists(ref, deployment.Metadata.NamespaceOrDefault())
}

func (s *SourceIndex) ListKind(kind string) []SourceRow {
	if s == nil {
		return nil
	}
	rows := s.items.List()
	out := make([]SourceRow, 0, len(rows))
	for _, row := range rows {
		if row.Key.Kind == kind {
			out = append(out, row)
		}
	}
	return out
}

func (s *SourceIndex) ResourceExists(ref v1alpha1.ResourceRef, fallbackNamespace string) bool {
	if s == nil {
		return false
	}
	key, ok := s.refKey(ref, fallbackNamespace)
	if !ok {
		return false
	}
	return s.items.GetKey(sourceObjectKey(key)) != nil
}

func (s *SourceIndex) store(kind string) *v1alpha1store.Store {
	if s == nil || s.stores == nil {
		return nil
	}
	return s.stores[kind]
}

func (s *SourceIndex) listAll(ctx context.Context, kind sourceKind) ([]SourceRow, error) {
	store := s.store(kind.Kind)
	if store == nil {
		return nil, fmt.Errorf("controller sources: no %s store registered", kind.Kind)
	}
	var out []SourceRow
	opts := v1alpha1store.ListOpts{Limit: listAllPageSize, IncludeTerminating: kind.IncludeTerminating}
	for {
		rows, cursor, err := store.List(ctx, opts)
		if err != nil {
			return nil, fmt.Errorf("controller sources: list %s: %w", kind.Kind, err)
		}
		for _, raw := range rows {
			obj, err := envelopeFromRegisteredRaw(raw, kind.Kind)
			if err != nil {
				return nil, fmt.Errorf("controller sources: decode %s: %w", kind.Kind, err)
			}
			out = append(out, SourceRow{Key: resourceKeyForObject(kind.Kind, obj), Object: obj})
		}
		if cursor == "" {
			return out, nil
		}
		opts.Cursor = cursor
	}
}

func (s *SourceIndex) applySourceEvent(ctx context.Context, kind sourceKind, event v1alpha1store.ControlPlaneEvent) error {
	store := s.store(kind.Kind)
	if store == nil {
		return fmt.Errorf("controller sources: no %s store registered", kind.Kind)
	}
	if event.Operation == "delete" {
		s.items.DeleteObject(sourceObjectKey(event.Key))
		return nil
	}

	var (
		raw *v1alpha1.RawObject
		err error
	)
	if kind.IncludeTerminating {
		raw, err = store.GetLatestIncludingTerminating(ctx, event.Key.Namespace, event.Key.Name)
	} else if store.Behavior() == v1alpha1store.TaggedArtifactStore {
		tag := event.Key.Tag
		if tag == "" {
			tag = v1alpha1store.DefaultTag()
		}
		raw, err = store.Get(ctx, event.Key.Namespace, event.Key.Name, tag)
	} else {
		raw, err = store.GetLatest(ctx, event.Key.Namespace, event.Key.Name)
	}
	if err != nil {
		if errors.Is(err, pkgdb.ErrNotFound) {
			s.items.DeleteObject(sourceObjectKey(event.Key))
			return nil
		}
		return fmt.Errorf("controller sources: load %s %s/%s: %w", kind.Kind, event.Key.Namespace, event.Key.Name, err)
	}
	obj, err := envelopeFromRegisteredRaw(raw, kind.Kind)
	if err != nil {
		return fmt.Errorf("controller sources: decode %s: %w", kind.Kind, err)
	}
	s.items.UpdateObject(SourceRow{Key: resourceKeyForObject(kind.Kind, obj), Object: obj})
	return nil
}

func (s *SourceIndex) refKey(ref v1alpha1.ResourceRef, fallbackNamespace string) (v1alpha1store.ResourceKey, bool) {
	if ref.Kind == "" || ref.Name == "" {
		return v1alpha1store.ResourceKey{}, false
	}
	if _, ok := s.kinds[ref.Kind]; !ok {
		return v1alpha1store.ResourceKey{}, false
	}
	key := v1alpha1store.ResourceKey{
		Kind:      ref.Kind,
		Namespace: refNamespace(ref.Namespace, fallbackNamespace),
		Name:      ref.Name,
		Tag:       ref.Tag,
	}
	if store := s.store(ref.Kind); store != nil && store.Behavior() == v1alpha1store.TaggedArtifactStore && key.Tag == "" {
		key.Tag = v1alpha1store.DefaultTag()
	}
	return key, true
}

func (s *SourceIndex) orderedKinds() []string {
	if s == nil {
		return nil
	}
	kinds := make([]string, 0, len(s.kinds))
	for kind := range s.kinds {
		kinds = append(kinds, kind)
	}
	slices.Sort(kinds)
	return kinds
}

func resourceKeyForObject(kind string, obj v1alpha1.Object) v1alpha1store.ResourceKey {
	meta := obj.GetMetadata()
	return v1alpha1store.ResourceKey{
		Kind:      kind,
		Namespace: meta.NamespaceOrDefault(),
		Name:      meta.Name,
		Tag:       meta.Tag,
	}
}

func envelopeFromRegisteredRaw(raw *v1alpha1.RawObject, kind string) (v1alpha1.Object, error) {
	if _, ok := newRegisteredObject(kind); !ok {
		return nil, fmt.Errorf("no v1alpha1 object registered for kind %s", kind)
	}
	return v1alpha1.EnvelopeFromRaw(func() v1alpha1.Object {
		obj, _ := newRegisteredObject(kind)
		return obj
	}, raw, kind)
}

func newRegisteredObject(kind string) (v1alpha1.Object, bool) {
	_, newObj, ok := v1alpha1.Default.Lookup(kind)
	if !ok {
		return nil, false
	}
	obj, ok := newObj().(v1alpha1.Object)
	return obj, ok
}

func refNamespace(refNamespace, fallback string) string {
	if refNamespace != "" {
		return refNamespace
	}
	if fallback != "" {
		return fallback
	}
	return v1alpha1.DefaultNamespace
}

func sourceObjectKey(key v1alpha1store.ResourceKey) string {
	return fmt.Sprintf("%s/%s/%s/%s", key.Kind, key.Namespace, key.Name, key.Tag)
}
