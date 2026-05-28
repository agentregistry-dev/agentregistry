package controller

import (
	"bytes"
	"cmp"
	"slices"

	"istio.io/istio/pkg/kube/krt"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

const (
	dependencyRoleRuntime        = "runtime"
	dependencyRoleTarget         = "target"
	dependencyRoleAgentMCPServer = "agent-mcpserver"
)

// DeploymentWorkIntent is the pure KRT projection from Deployment source state
// and its fetched dependencies into durable work that a leaf handler can
// upsert. Dependencies are included so changes to referenced resources emit a
// fresh intent even when the durable work key is unchanged.
type DeploymentWorkIntent struct {
	Work         v1alpha1store.ReconcileWork
	Dependencies []DeploymentWorkDependency
}

func (i DeploymentWorkIntent) ResourceName() string {
	return i.Work.Key
}

func (i DeploymentWorkIntent) Equals(other DeploymentWorkIntent) bool {
	return i.Work.Key == other.Work.Key &&
		i.Work.Resource == other.Work.Resource &&
		i.Work.UID == other.Work.UID &&
		i.Work.Generation == other.Work.Generation &&
		i.Work.Action == other.Work.Action &&
		i.Work.Reason == other.Work.Reason &&
		bytes.Equal(i.Work.Payload, other.Work.Payload) &&
		slices.Equal(i.Dependencies, other.Dependencies)
}

// DeploymentWorkDependency records the KRT-visible source rows that influenced
// an intent. It is not persisted; it exists to make dependency changes observable
// to KRT's equality check and event propagation.
type DeploymentWorkDependency struct {
	Role       string
	Key        v1alpha1store.ResourceKey
	UID        string
	Generation int64
	Missing    bool
}

// NewDeploymentWorkIntents builds the KRT-native deployment derivation graph.
// Source collection changes and fetched dependency changes both recompute the
// affected Deployment intent; side effects remain in the registered leaf
// handler owned by DeploymentWorkDeriver.
func NewDeploymentWorkIntents(sources *SourceIndex) krt.Collection[DeploymentWorkIntent] {
	return krt.NewCollection[DeploymentSource, DeploymentWorkIntent](
		sources.Deployments,
		func(ctx krt.HandlerContext, row DeploymentSource) *DeploymentWorkIntent {
			return buildDeploymentWorkIntent(ctx, sources, row)
		},
		krt.WithName("agentregistry/controller/deployment-work-intents"),
	)
}

func buildDeploymentWorkIntent(ctx krt.HandlerContext, sources *SourceIndex, row DeploymentSource) *DeploymentWorkIntent {
	if sources == nil || row.Deployment == nil {
		return nil
	}
	work, err := DeriveDeploymentWork(row.Deployment)
	if err != nil {
		return nil
	}

	dependencies := make([]DeploymentWorkDependency, 0, 4)
	runtimeDep, runtimeExists := sources.fetchRuntimeDependency(ctx, row.Deployment)
	if runtimeDep.Key.Kind != "" {
		dependencies = append(dependencies, runtimeDep)
	}
	targetDep, targetExists, targetAgent := sources.fetchTargetDependency(ctx, row.Deployment)
	if targetDep.Key.Kind != "" {
		dependencies = append(dependencies, targetDep)
	}
	if targetAgent != nil {
		dependencies = append(dependencies, sources.fetchAgentMCPServerDependencies(ctx, *targetAgent)...)
	}
	sortDependencies(dependencies)

	if work.Action == ReconcileActionApply || work.Action == ReconcileActionRemove {
		switch {
		case !runtimeExists:
			work.Reason = "runtime-reference-pending"
		case !targetExists && work.Action == ReconcileActionApply:
			work.Reason = "target-reference-pending"
		}
	}

	return &DeploymentWorkIntent{Work: work, Dependencies: dependencies}
}

func (s *SourceIndex) fetchRuntimeDependency(
	ctx krt.HandlerContext,
	deployment *v1alpha1.Deployment,
) (DeploymentWorkDependency, bool) {
	key, ok := s.runtimeRefKey(deployment)
	if !ok {
		return DeploymentWorkDependency{}, false
	}
	row := krt.FetchOne(ctx, s.Runtimes, krt.FilterKey(sourceObjectKey(key)))
	if row == nil {
		return missingDependency(dependencyRoleRuntime, key), false
	}
	return dependencyFromObject(dependencyRoleRuntime, row.Key, row.Runtime), true
}

func (s *SourceIndex) fetchTargetDependency(
	ctx krt.HandlerContext,
	deployment *v1alpha1.Deployment,
) (DeploymentWorkDependency, bool, *AgentSource) {
	key, ok := s.targetRefKey(deployment)
	if !ok {
		return DeploymentWorkDependency{}, false, nil
	}
	switch key.Kind {
	case v1alpha1.KindAgent:
		row := krt.FetchOne(ctx, s.Agents, krt.FilterKey(sourceObjectKey(key)))
		if row == nil {
			return missingDependency(dependencyRoleTarget, key), false, nil
		}
		return dependencyFromObject(dependencyRoleTarget, row.Key, row.Agent), true, row
	case v1alpha1.KindMCPServer:
		row := krt.FetchOne(ctx, s.MCPServers, krt.FilterKey(sourceObjectKey(key)))
		if row == nil {
			return missingDependency(dependencyRoleTarget, key), false, nil
		}
		return dependencyFromObject(dependencyRoleTarget, row.Key, row.MCPServer), true, nil
	default:
		return missingDependency(dependencyRoleTarget, key), false, nil
	}
}

func (s *SourceIndex) fetchAgentMCPServerDependencies(
	ctx krt.HandlerContext,
	agent AgentSource,
) []DeploymentWorkDependency {
	keys := s.agentMCPServerRefKeys(agent.Agent)
	dependencies := make([]DeploymentWorkDependency, 0, len(keys))
	for _, key := range keys {
		row := krt.FetchOne(ctx, s.MCPServers, krt.FilterKey(sourceObjectKey(key)))
		if row == nil {
			dependencies = append(dependencies, missingDependency(dependencyRoleAgentMCPServer, key))
			continue
		}
		dependencies = append(dependencies, dependencyFromObject(dependencyRoleAgentMCPServer, row.Key, row.MCPServer))
	}
	return dependencies
}

func dependencyFromObject(role string, key v1alpha1store.ResourceKey, obj v1alpha1.Object) DeploymentWorkDependency {
	dependency := DeploymentWorkDependency{Role: role, Key: key}
	if obj == nil {
		dependency.Missing = true
		return dependency
	}
	meta := obj.GetMetadata()
	dependency.UID = meta.UID
	dependency.Generation = meta.Generation
	return dependency
}

func missingDependency(role string, key v1alpha1store.ResourceKey) DeploymentWorkDependency {
	return DeploymentWorkDependency{Role: role, Key: key, Missing: true}
}

func sortDependencies(dependencies []DeploymentWorkDependency) {
	slices.SortFunc(dependencies, func(a, b DeploymentWorkDependency) int {
		if role := cmp.Compare(a.Role, b.Role); role != 0 {
			return role
		}
		if kind := cmp.Compare(a.Key.Kind, b.Key.Kind); kind != 0 {
			return kind
		}
		if namespace := cmp.Compare(a.Key.Namespace, b.Key.Namespace); namespace != 0 {
			return namespace
		}
		if name := cmp.Compare(a.Key.Name, b.Key.Name); name != 0 {
			return name
		}
		return cmp.Compare(a.Key.Tag, b.Key.Tag)
	})
}
