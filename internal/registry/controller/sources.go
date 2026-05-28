package controller

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"istio.io/istio/pkg/kube/krt"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

const listAllPageSize = 500

// DeploymentSource is the typed KRT source row for Deployment intent.
type DeploymentSource struct {
	Key        v1alpha1store.ResourceKey
	Deployment *v1alpha1.Deployment
}

func (r DeploymentSource) ResourceName() string {
	return sourceObjectKey(r.Key)
}

type RuntimeSource struct {
	Key     v1alpha1store.ResourceKey
	Runtime *v1alpha1.Runtime
}

func (r RuntimeSource) ResourceName() string {
	return sourceObjectKey(r.Key)
}

type AgentSource struct {
	Key   v1alpha1store.ResourceKey
	Agent *v1alpha1.Agent
}

func (r AgentSource) ResourceName() string {
	return sourceObjectKey(r.Key)
}

type MCPServerSource struct {
	Key       v1alpha1store.ResourceKey
	MCPServer *v1alpha1.MCPServer
}

func (r MCPServerSource) ResourceName() string {
	return sourceObjectKey(r.Key)
}

type SkillSource struct {
	Key   v1alpha1store.ResourceKey
	Skill *v1alpha1.Skill
}

func (r SkillSource) ResourceName() string {
	return sourceObjectKey(r.Key)
}

type PromptSource struct {
	Key    v1alpha1store.ResourceKey
	Prompt *v1alpha1.Prompt
}

func (r PromptSource) ResourceName() string {
	return sourceObjectKey(r.Key)
}

type sourceKind struct {
	Kind               string
	IncludeTerminating bool
}

type sourceSyncer struct {
	synced chan struct{}
}

func newSourceSyncer() *sourceSyncer {
	return &sourceSyncer{synced: make(chan struct{})}
}

func (s *sourceSyncer) WaitUntilSynced(stop <-chan struct{}) bool {
	select {
	case <-s.synced:
		return true
	case <-stop:
		return false
	}
}

func (s *sourceSyncer) HasSynced() bool {
	select {
	case <-s.synced:
		return true
	default:
		return false
	}
}

func (s *sourceSyncer) markSynced() {
	select {
	case <-s.synced:
	default:
		close(s.synced)
	}
}

// SourceIndex is the controller-owned KRT read graph for v1alpha1 source
// objects. The collections stay typed so derived KRT collections can fetch
// exactly the resource families they depend on.
type SourceIndex struct {
	stores map[string]*v1alpha1store.Store
	kinds  map[string]sourceKind
	syncer *sourceSyncer

	Deployments krt.StaticCollection[DeploymentSource]
	Runtimes    krt.StaticCollection[RuntimeSource]
	Agents      krt.StaticCollection[AgentSource]
	MCPServers  krt.StaticCollection[MCPServerSource]
	Skills      krt.StaticCollection[SkillSource]
	Prompts     krt.StaticCollection[PromptSource]
}

// SourceIndexOptions configures generic source projection behavior.
type SourceIndexOptions struct {
	InitialFinalizers map[string]func(v1alpha1.Object) []string
}

// NewSourceIndex creates a typed KRT source read model for registered v1alpha1
// stores. Enterprise extensions can add more collections on top of this OSS
// graph, but the built-in Deployment controller no longer relies on a generic
// all-kinds bag.
func NewSourceIndex(stores map[string]*v1alpha1store.Store, opts ...SourceIndexOptions) *SourceIndex {
	var options SourceIndexOptions
	if len(opts) > 0 {
		options = opts[0]
	}
	kinds := make(map[string]sourceKind, len(stores))
	for kind := range stores {
		if _, ok := newRegisteredObject(kind); !ok {
			continue
		}
		kinds[kind] = sourceKind{
			Kind:               kind,
			IncludeTerminating: hasInitialFinalizer(kind, options.InitialFinalizers),
		}
	}
	syncer := newSourceSyncer()
	s := &SourceIndex{
		stores: stores,
		kinds:  kinds,
		syncer: syncer,
		Deployments: krt.NewStaticCollection[DeploymentSource](syncer, nil,
			krt.WithName("agentregistry/source/deployments")),
		Runtimes: krt.NewStaticCollection[RuntimeSource](syncer, nil,
			krt.WithName("agentregistry/source/runtimes")),
		Agents: krt.NewStaticCollection[AgentSource](syncer, nil,
			krt.WithName("agentregistry/source/agents")),
		MCPServers: krt.NewStaticCollection[MCPServerSource](syncer, nil,
			krt.WithName("agentregistry/source/mcpservers")),
		Skills: krt.NewStaticCollection[SkillSource](syncer, nil,
			krt.WithName("agentregistry/source/skills")),
		Prompts: krt.NewStaticCollection[PromptSource](syncer, nil,
			krt.WithName("agentregistry/source/prompts")),
	}
	return s
}

func hasInitialFinalizer(kind string, finalizers map[string]func(v1alpha1.Object) []string) bool {
	return finalizers[kind] != nil
}

// Refresh rebuilds every source collection from canonical store tables.
func (s *SourceIndex) Refresh(ctx context.Context) error {
	if s == nil {
		return errors.New("controller sources: sources are required")
	}
	for _, kind := range s.orderedKinds() {
		if err := s.refreshKind(ctx, s.kinds[kind]); err != nil {
			return err
		}
	}
	s.markSynced()
	return nil
}

func (s *SourceIndex) markSynced() {
	if s != nil && s.syncer != nil {
		s.syncer.markSynced()
	}
}

func (s *SourceIndex) refreshKind(ctx context.Context, kind sourceKind) error {
	switch kind.Kind {
	case v1alpha1.KindDeployment:
		rows, err := s.listDeployments(ctx, kind)
		if err != nil {
			return err
		}
		s.Deployments.Reset(rows)
	case v1alpha1.KindRuntime:
		rows, err := s.listRuntimes(ctx, kind)
		if err != nil {
			return err
		}
		s.Runtimes.Reset(rows)
	case v1alpha1.KindAgent:
		rows, err := s.listAgents(ctx, kind)
		if err != nil {
			return err
		}
		s.Agents.Reset(rows)
	case v1alpha1.KindMCPServer:
		rows, err := s.listMCPServers(ctx, kind)
		if err != nil {
			return err
		}
		s.MCPServers.Reset(rows)
	case v1alpha1.KindSkill:
		rows, err := s.listSkills(ctx, kind)
		if err != nil {
			return err
		}
		s.Skills.Reset(rows)
	case v1alpha1.KindPrompt:
		rows, err := s.listPrompts(ctx, kind)
		if err != nil {
			return err
		}
		s.Prompts.Reset(rows)
	}
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
	switch kind.Kind {
	case v1alpha1.KindDeployment:
		return s.applyDeploymentEvent(ctx, kind, event)
	case v1alpha1.KindRuntime:
		return s.applyRuntimeEvent(ctx, kind, event)
	case v1alpha1.KindAgent:
		return s.applyAgentEvent(ctx, kind, event)
	case v1alpha1.KindMCPServer:
		return s.applyMCPServerEvent(ctx, kind, event)
	case v1alpha1.KindSkill:
		return s.applySkillEvent(ctx, kind, event)
	case v1alpha1.KindPrompt:
		return s.applyPromptEvent(ctx, kind, event)
	default:
		return nil
	}
}

func (s *SourceIndex) store(kind string) *v1alpha1store.Store {
	if s == nil || s.stores == nil {
		return nil
	}
	return s.stores[kind]
}

func (s *SourceIndex) listDeployments(ctx context.Context, kind sourceKind) ([]DeploymentSource, error) {
	return listTyped(ctx, s, kind, func() *v1alpha1.Deployment { return &v1alpha1.Deployment{} }, func(key v1alpha1store.ResourceKey, obj *v1alpha1.Deployment) DeploymentSource {
		return DeploymentSource{Key: key, Deployment: obj}
	})
}

func (s *SourceIndex) listRuntimes(ctx context.Context, kind sourceKind) ([]RuntimeSource, error) {
	return listTyped(ctx, s, kind, func() *v1alpha1.Runtime { return &v1alpha1.Runtime{} }, func(key v1alpha1store.ResourceKey, obj *v1alpha1.Runtime) RuntimeSource {
		return RuntimeSource{Key: key, Runtime: obj}
	})
}

func (s *SourceIndex) listAgents(ctx context.Context, kind sourceKind) ([]AgentSource, error) {
	return listTyped(ctx, s, kind, func() *v1alpha1.Agent { return &v1alpha1.Agent{} }, func(key v1alpha1store.ResourceKey, obj *v1alpha1.Agent) AgentSource {
		return AgentSource{Key: key, Agent: obj}
	})
}

func (s *SourceIndex) listMCPServers(ctx context.Context, kind sourceKind) ([]MCPServerSource, error) {
	return listTyped(ctx, s, kind, func() *v1alpha1.MCPServer { return &v1alpha1.MCPServer{} }, func(key v1alpha1store.ResourceKey, obj *v1alpha1.MCPServer) MCPServerSource {
		return MCPServerSource{Key: key, MCPServer: obj}
	})
}

func (s *SourceIndex) listSkills(ctx context.Context, kind sourceKind) ([]SkillSource, error) {
	return listTyped(ctx, s, kind, func() *v1alpha1.Skill { return &v1alpha1.Skill{} }, func(key v1alpha1store.ResourceKey, obj *v1alpha1.Skill) SkillSource {
		return SkillSource{Key: key, Skill: obj}
	})
}

func (s *SourceIndex) listPrompts(ctx context.Context, kind sourceKind) ([]PromptSource, error) {
	return listTyped(ctx, s, kind, func() *v1alpha1.Prompt { return &v1alpha1.Prompt{} }, func(key v1alpha1store.ResourceKey, obj *v1alpha1.Prompt) PromptSource {
		return PromptSource{Key: key, Prompt: obj}
	})
}

func listTyped[T v1alpha1.Object, R any](
	ctx context.Context,
	s *SourceIndex,
	kind sourceKind,
	newObj func() T,
	wrap func(v1alpha1store.ResourceKey, T) R,
) ([]R, error) {
	store := s.store(kind.Kind)
	if store == nil {
		return nil, fmt.Errorf("controller sources: no %s store registered", kind.Kind)
	}
	var out []R
	opts := v1alpha1store.ListOpts{Limit: listAllPageSize, IncludeTerminating: kind.IncludeTerminating}
	for {
		rows, cursor, err := store.List(ctx, opts)
		if err != nil {
			return nil, fmt.Errorf("controller sources: list %s: %w", kind.Kind, err)
		}
		for _, raw := range rows {
			obj, err := v1alpha1.EnvelopeFromRaw(newObj, raw, kind.Kind)
			if err != nil {
				return nil, fmt.Errorf("controller sources: decode %s: %w", kind.Kind, err)
			}
			out = append(out, wrap(resourceKeyForObject(kind.Kind, obj), obj))
		}
		if cursor == "" {
			return out, nil
		}
		opts.Cursor = cursor
	}
}

func (s *SourceIndex) applyDeploymentEvent(ctx context.Context, kind sourceKind, event v1alpha1store.ControlPlaneEvent) error {
	return applyTypedEvent(ctx, s, kind, event, s.Deployments, func() *v1alpha1.Deployment { return &v1alpha1.Deployment{} }, func(key v1alpha1store.ResourceKey, obj *v1alpha1.Deployment) DeploymentSource {
		return DeploymentSource{Key: key, Deployment: obj}
	})
}

func (s *SourceIndex) applyRuntimeEvent(ctx context.Context, kind sourceKind, event v1alpha1store.ControlPlaneEvent) error {
	return applyTypedEvent(ctx, s, kind, event, s.Runtimes, func() *v1alpha1.Runtime { return &v1alpha1.Runtime{} }, func(key v1alpha1store.ResourceKey, obj *v1alpha1.Runtime) RuntimeSource {
		return RuntimeSource{Key: key, Runtime: obj}
	})
}

func (s *SourceIndex) applyAgentEvent(ctx context.Context, kind sourceKind, event v1alpha1store.ControlPlaneEvent) error {
	return applyTypedEvent(ctx, s, kind, event, s.Agents, func() *v1alpha1.Agent { return &v1alpha1.Agent{} }, func(key v1alpha1store.ResourceKey, obj *v1alpha1.Agent) AgentSource {
		return AgentSource{Key: key, Agent: obj}
	})
}

func (s *SourceIndex) applyMCPServerEvent(ctx context.Context, kind sourceKind, event v1alpha1store.ControlPlaneEvent) error {
	return applyTypedEvent(ctx, s, kind, event, s.MCPServers, func() *v1alpha1.MCPServer { return &v1alpha1.MCPServer{} }, func(key v1alpha1store.ResourceKey, obj *v1alpha1.MCPServer) MCPServerSource {
		return MCPServerSource{Key: key, MCPServer: obj}
	})
}

func (s *SourceIndex) applySkillEvent(ctx context.Context, kind sourceKind, event v1alpha1store.ControlPlaneEvent) error {
	return applyTypedEvent(ctx, s, kind, event, s.Skills, func() *v1alpha1.Skill { return &v1alpha1.Skill{} }, func(key v1alpha1store.ResourceKey, obj *v1alpha1.Skill) SkillSource {
		return SkillSource{Key: key, Skill: obj}
	})
}

func (s *SourceIndex) applyPromptEvent(ctx context.Context, kind sourceKind, event v1alpha1store.ControlPlaneEvent) error {
	return applyTypedEvent(ctx, s, kind, event, s.Prompts, func() *v1alpha1.Prompt { return &v1alpha1.Prompt{} }, func(key v1alpha1store.ResourceKey, obj *v1alpha1.Prompt) PromptSource {
		return PromptSource{Key: key, Prompt: obj}
	})
}

func applyTypedEvent[T v1alpha1.Object, R interface{ ResourceName() string }](
	ctx context.Context,
	s *SourceIndex,
	kind sourceKind,
	event v1alpha1store.ControlPlaneEvent,
	collection krt.StaticCollection[R],
	newObj func() T,
	wrap func(v1alpha1store.ResourceKey, T) R,
) error {
	store := s.store(kind.Kind)
	if store == nil {
		return fmt.Errorf("controller sources: no %s store registered", kind.Kind)
	}
	if event.Operation == "delete" {
		collection.DeleteObject(sourceObjectKey(event.Key))
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
			collection.DeleteObject(sourceObjectKey(event.Key))
			return nil
		}
		return fmt.Errorf("controller sources: load %s %s/%s: %w", kind.Kind, event.Key.Namespace, event.Key.Name, err)
	}
	obj, err := v1alpha1.EnvelopeFromRaw(newObj, raw, kind.Kind)
	if err != nil {
		return fmt.Errorf("controller sources: decode %s: %w", kind.Kind, err)
	}
	collection.ConditionalUpdateObject(wrap(resourceKeyForObject(kind.Kind, obj), obj))
	return nil
}

func (s *SourceIndex) runtimeRefKey(deployment *v1alpha1.Deployment) (v1alpha1store.ResourceKey, bool) {
	if deployment == nil {
		return v1alpha1store.ResourceKey{}, false
	}
	ref := deployment.Spec.RuntimeRef
	ref.Kind = v1alpha1.KindRuntime
	return s.refKey(ref, deployment.Metadata.NamespaceOrDefault())
}

func (s *SourceIndex) targetRefKey(deployment *v1alpha1.Deployment) (v1alpha1store.ResourceKey, bool) {
	if deployment == nil {
		return v1alpha1store.ResourceKey{}, false
	}
	return s.refKey(deployment.Spec.TargetRef, deployment.Metadata.NamespaceOrDefault())
}

func (s *SourceIndex) agentMCPServerRefKeys(agent *v1alpha1.Agent) []v1alpha1store.ResourceKey {
	if agent == nil {
		return nil
	}
	keys := make([]v1alpha1store.ResourceKey, 0, len(agent.Spec.MCPServers))
	for _, ref := range agent.Spec.MCPServers {
		if ref.Kind == "" {
			ref.Kind = v1alpha1.KindMCPServer
		}
		key, ok := s.refKey(ref, agent.Metadata.NamespaceOrDefault())
		if ok {
			keys = append(keys, key)
		}
	}
	return keys
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
