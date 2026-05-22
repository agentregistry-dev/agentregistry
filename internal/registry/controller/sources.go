package controller

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
	"istio.io/istio/pkg/kube/krt"
)

const listAllPageSize = 500

// SourceRow is a typed KRT source row keyed by v1alpha1 resource identity.
type SourceRow[T v1alpha1.Object] struct {
	Key    v1alpha1store.ResourceKey
	Object T
}

// ResourceName implements krt.ResourceNamer.
func (r SourceRow[T]) ResourceName() string {
	return sourceObjectKey(r.Key)
}

// DeploymentSources holds the first controller-owned read model: Deployments,
// their Runtimes, and the Deployment target artifact kinds.
type DeploymentSources struct {
	stores map[string]*v1alpha1store.Store

	Deployments krt.StaticCollection[SourceRow[*v1alpha1.Deployment]]
	Runtimes    krt.StaticCollection[SourceRow[*v1alpha1.Runtime]]
	Agents      krt.StaticCollection[SourceRow[*v1alpha1.Agent]]
	MCPServers  krt.StaticCollection[SourceRow[*v1alpha1.MCPServer]]
}

func NewDeploymentSources(stores map[string]*v1alpha1store.Store) *DeploymentSources {
	return &DeploymentSources{
		stores:      stores,
		Deployments: krt.NewStaticCollection[SourceRow[*v1alpha1.Deployment]](nil, nil, krt.WithName("agentregistry-deployments")),
		Runtimes:    krt.NewStaticCollection[SourceRow[*v1alpha1.Runtime]](nil, nil, krt.WithName("agentregistry-runtimes")),
		Agents:      krt.NewStaticCollection[SourceRow[*v1alpha1.Agent]](nil, nil, krt.WithName("agentregistry-agents")),
		MCPServers:  krt.NewStaticCollection[SourceRow[*v1alpha1.MCPServer]](nil, nil, krt.WithName("agentregistry-mcpservers")),
	}
}

// Refresh rebuilds every source collection from canonical store tables.
func (s *DeploymentSources) Refresh(ctx context.Context) error {
	if s == nil {
		return errors.New("controller sources: sources are required")
	}
	deployments, err := listAllTyped(ctx, s.store(v1alpha1.KindDeployment), v1alpha1.KindDeployment, true, func() *v1alpha1.Deployment {
		return &v1alpha1.Deployment{}
	})
	if err != nil {
		return err
	}
	runtimes, err := listAllTyped(ctx, s.store(v1alpha1.KindRuntime), v1alpha1.KindRuntime, false, func() *v1alpha1.Runtime {
		return &v1alpha1.Runtime{}
	})
	if err != nil {
		return err
	}
	agents, err := listAllTyped(ctx, s.store(v1alpha1.KindAgent), v1alpha1.KindAgent, false, func() *v1alpha1.Agent {
		return &v1alpha1.Agent{}
	})
	if err != nil {
		return err
	}
	mcpServers, err := listAllTyped(ctx, s.store(v1alpha1.KindMCPServer), v1alpha1.KindMCPServer, false, func() *v1alpha1.MCPServer {
		return &v1alpha1.MCPServer{}
	})
	if err != nil {
		return err
	}

	s.Deployments.Reset(deployments)
	s.Runtimes.Reset(runtimes)
	s.Agents.Reset(agents)
	s.MCPServers.Reset(mcpServers)
	return nil
}

// ApplyEvent incrementally refreshes one projected source row.
func (s *DeploymentSources) ApplyEvent(ctx context.Context, event v1alpha1store.ControlPlaneEvent) error {
	if s == nil {
		return errors.New("controller sources: sources are required")
	}
	switch event.Key.Kind {
	case v1alpha1.KindDeployment:
		return applySourceEvent(ctx, s.store(v1alpha1.KindDeployment), event, true, &s.Deployments, func() *v1alpha1.Deployment {
			return &v1alpha1.Deployment{}
		})
	case v1alpha1.KindRuntime:
		return applySourceEvent(ctx, s.store(v1alpha1.KindRuntime), event, true, &s.Runtimes, func() *v1alpha1.Runtime {
			return &v1alpha1.Runtime{}
		})
	case v1alpha1.KindAgent:
		return applySourceEvent(ctx, s.store(v1alpha1.KindAgent), event, false, &s.Agents, func() *v1alpha1.Agent {
			return &v1alpha1.Agent{}
		})
	case v1alpha1.KindMCPServer:
		return applySourceEvent(ctx, s.store(v1alpha1.KindMCPServer), event, false, &s.MCPServers, func() *v1alpha1.MCPServer {
			return &v1alpha1.MCPServer{}
		})
	default:
		return nil
	}
}

func (s *DeploymentSources) DeploymentList() []SourceRow[*v1alpha1.Deployment] {
	if s == nil {
		return nil
	}
	return s.Deployments.List()
}

func (s *DeploymentSources) TargetExists(deployment *v1alpha1.Deployment) bool {
	if s == nil || deployment == nil {
		return false
	}
	ref := deployment.Spec.TargetRef
	ref.Namespace = refNamespace(ref.Namespace, deployment.Metadata.NamespaceOrDefault())
	key := v1alpha1store.ResourceKey{Kind: ref.Kind, Namespace: ref.Namespace, Name: ref.Name, Tag: ref.Tag}
	if key.Tag == "" {
		key.Tag = v1alpha1store.DefaultTag()
	}
	switch ref.Kind {
	case v1alpha1.KindAgent:
		return s.Agents.GetKey(sourceObjectKey(key)) != nil
	case v1alpha1.KindMCPServer:
		return s.MCPServers.GetKey(sourceObjectKey(key)) != nil
	default:
		return false
	}
}

func (s *DeploymentSources) RuntimeExists(deployment *v1alpha1.Deployment) bool {
	if s == nil || deployment == nil {
		return false
	}
	ref := deployment.Spec.RuntimeRef
	ref.Namespace = refNamespace(ref.Namespace, deployment.Metadata.NamespaceOrDefault())
	key := v1alpha1store.ResourceKey{Kind: v1alpha1.KindRuntime, Namespace: ref.Namespace, Name: ref.Name}
	return s.Runtimes.GetKey(sourceObjectKey(key)) != nil
}

func (s *DeploymentSources) store(kind string) *v1alpha1store.Store {
	if s == nil || s.stores == nil {
		return nil
	}
	return s.stores[kind]
}

func listAllTyped[T v1alpha1.Object](
	ctx context.Context,
	store *v1alpha1store.Store,
	kind string,
	includeTerminating bool,
	newObj func() T,
) ([]SourceRow[T], error) {
	if store == nil {
		return nil, fmt.Errorf("controller sources: no %s store registered", kind)
	}
	var out []SourceRow[T]
	opts := v1alpha1store.ListOpts{Limit: listAllPageSize, IncludeTerminating: includeTerminating}
	for {
		rows, cursor, err := store.List(ctx, opts)
		if err != nil {
			return nil, fmt.Errorf("controller sources: list %s: %w", kind, err)
		}
		for _, raw := range rows {
			obj, err := v1alpha1.EnvelopeFromRaw(newObj, raw, kind)
			if err != nil {
				return nil, fmt.Errorf("controller sources: decode %s: %w", kind, err)
			}
			out = append(out, SourceRow[T]{Key: resourceKeyForObject(kind, obj), Object: obj})
		}
		if cursor == "" {
			return out, nil
		}
		opts.Cursor = cursor
	}
}

func applySourceEvent[T v1alpha1.Object](
	ctx context.Context,
	store *v1alpha1store.Store,
	event v1alpha1store.ControlPlaneEvent,
	mutable bool,
	collection *krt.StaticCollection[SourceRow[T]],
	newObj func() T,
) error {
	if collection == nil {
		return nil
	}
	if store == nil {
		return fmt.Errorf("controller sources: no %s store registered", event.Key.Kind)
	}
	if event.Operation == "delete" {
		collection.DeleteObject(sourceObjectKey(event.Key))
		return nil
	}

	var (
		raw *v1alpha1.RawObject
		err error
	)
	if mutable {
		raw, err = store.GetLatestIncludingTerminating(ctx, event.Key.Namespace, event.Key.Name)
	} else {
		raw, err = store.Get(ctx, event.Key.Namespace, event.Key.Name, event.Key.Tag)
	}
	if err != nil {
		if errors.Is(err, pkgdb.ErrNotFound) {
			collection.DeleteObject(sourceObjectKey(event.Key))
			return nil
		}
		return fmt.Errorf("controller sources: load %s %s/%s: %w", event.Key.Kind, event.Key.Namespace, event.Key.Name, err)
	}
	obj, err := v1alpha1.EnvelopeFromRaw(newObj, raw, event.Key.Kind)
	if err != nil {
		return fmt.Errorf("controller sources: decode %s: %w", event.Key.Kind, err)
	}
	collection.UpdateObject(SourceRow[T]{Key: resourceKeyForObject(event.Key.Kind, obj), Object: obj})
	return nil
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

func refNamespace(refNamespace, fallback string) string {
	if refNamespace != "" {
		return refNamespace
	}
	if fallback != "" {
		return fallback
	}
	return v1alpha1.DefaultNamespace
}
