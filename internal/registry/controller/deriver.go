package controller

import (
	"context"
	"log/slog"
	"time"

	"istio.io/istio/pkg/kube/krt"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

// DeploymentWorkDeriver turns projected source state into durable work. It
// deliberately does not call adapters; side effects begin only after a work
// claim is leased.
type DeploymentWorkDeriver struct {
	Sources *SourceIndex
	Work    *v1alpha1store.ReconcileWorkStore
	Now     func() time.Time
}

// RegisterKRTHandlers wires deployment work derivation to the typed KRT source
// graph. It is the normal incremental path: Deployment changes derive that
// Deployment, while Runtime/target changes derive only indexed dependents.
func (d *DeploymentWorkDeriver) RegisterKRTHandlers(ctx context.Context) []krt.HandlerRegistration {
	if d == nil || d.Sources == nil {
		return nil
	}
	return []krt.HandlerRegistration{
		d.Sources.Deployments.RegisterBatch(func(events []krt.Event[DeploymentSource]) {
			if _, err := d.DeriveDeploymentEvents(ctx, events); err != nil {
				slog.Error("deployment controller derivation failed", "source", "deployment", "error", err)
			}
		}, false),
		d.Sources.Runtimes.RegisterBatch(func(events []krt.Event[RuntimeSource]) {
			if _, err := d.DeriveRuntimeEvents(ctx, events); err != nil {
				slog.Error("deployment controller derivation failed", "source", "runtime", "error", err)
			}
		}, false),
		d.Sources.Agents.RegisterBatch(func(events []krt.Event[AgentSource]) {
			if _, err := d.DeriveTargetEvents(ctx, agentEventKeys(events)); err != nil {
				slog.Error("deployment controller derivation failed", "source", "agent", "error", err)
			}
		}, false),
		d.Sources.MCPServers.RegisterBatch(func(events []krt.Event[MCPServerSource]) {
			if _, err := d.DeriveMCPServerEvents(ctx, events); err != nil {
				slog.Error("deployment controller derivation failed", "source", "mcpserver", "error", err)
			}
		}, false),
	}
}

// DeriveAll is the repair/bootstrap path. Incremental work should flow through
// KRT collection handlers so dependency changes rederive only affected
// Deployments.
func (d *DeploymentWorkDeriver) DeriveAll(ctx context.Context) (int, error) {
	if d == nil || d.Sources == nil {
		return 0, nil
	}
	count := 0
	for _, row := range d.Sources.DeploymentList() {
		if row.Deployment == nil {
			continue
		}
		if err := d.DeriveDeployment(ctx, row.Deployment); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func (d *DeploymentWorkDeriver) DeriveDeploymentEvents(ctx context.Context, events []krt.Event[DeploymentSource]) (int, error) {
	count := 0
	for _, event := range events {
		if event.New == nil || event.New.Deployment == nil {
			continue
		}
		if err := d.DeriveDeployment(ctx, event.New.Deployment); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func (d *DeploymentWorkDeriver) DeriveRuntimeEvents(ctx context.Context, events []krt.Event[RuntimeSource]) (int, error) {
	return d.deriveForRuntimeKeys(ctx, runtimeEventKeys(events))
}

func (d *DeploymentWorkDeriver) DeriveMCPServerEvents(ctx context.Context, events []krt.Event[MCPServerSource]) (int, error) {
	return d.deriveForMCPServerKeys(ctx, mcpServerEventKeys(events))
}

func (d *DeploymentWorkDeriver) DeriveTargetEvents(ctx context.Context, keys []v1alpha1store.ResourceKey) (int, error) {
	if d == nil || d.Sources == nil {
		return 0, nil
	}
	deployments := make(map[string]*v1alpha1.Deployment)
	for _, key := range keys {
		for _, row := range d.Sources.DeploymentsForTarget(key) {
			if row.Deployment != nil {
				deployments[row.ResourceName()] = row.Deployment
			}
		}
	}
	return d.deriveDeploymentSet(ctx, deployments)
}

func (d *DeploymentWorkDeriver) deriveForMCPServerKeys(ctx context.Context, keys []v1alpha1store.ResourceKey) (int, error) {
	if d == nil || d.Sources == nil {
		return 0, nil
	}
	deployments := make(map[string]*v1alpha1.Deployment)
	for _, key := range keys {
		for _, row := range d.Sources.DeploymentsForTarget(key) {
			if row.Deployment != nil {
				deployments[row.ResourceName()] = row.Deployment
			}
		}
		for _, agent := range d.Sources.AgentsForMCPServer(key) {
			for _, row := range d.Sources.DeploymentsForTarget(agent.Key) {
				if row.Deployment != nil {
					deployments[row.ResourceName()] = row.Deployment
				}
			}
		}
	}
	return d.deriveDeploymentSet(ctx, deployments)
}

func (d *DeploymentWorkDeriver) deriveForRuntimeKeys(ctx context.Context, keys []v1alpha1store.ResourceKey) (int, error) {
	if d == nil || d.Sources == nil {
		return 0, nil
	}
	deployments := make(map[string]*v1alpha1.Deployment)
	for _, key := range keys {
		for _, row := range d.Sources.DeploymentsForRuntime(key) {
			if row.Deployment != nil {
				deployments[row.ResourceName()] = row.Deployment
			}
		}
	}
	return d.deriveDeploymentSet(ctx, deployments)
}

func (d *DeploymentWorkDeriver) deriveDeploymentSet(ctx context.Context, deployments map[string]*v1alpha1.Deployment) (int, error) {
	count := 0
	for _, deployment := range deployments {
		if err := d.DeriveDeployment(ctx, deployment); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func (d *DeploymentWorkDeriver) DeriveDeployment(ctx context.Context, deployment *v1alpha1.Deployment) error {
	if d == nil || d.Work == nil {
		return nil
	}
	work, err := DeriveDeploymentWork(deployment)
	if err != nil {
		return err
	}
	if work.NextAttemptAt.IsZero() {
		work.NextAttemptAt = d.now()
	}
	if d.Sources != nil && (work.Action == ReconcileActionApply || work.Action == ReconcileActionRemove) {
		switch {
		case !d.Sources.RuntimeExists(deployment):
			work.Reason = "runtime-reference-pending"
		case !d.Sources.TargetExists(deployment) && work.Action == ReconcileActionApply:
			work.Reason = "target-reference-pending"
		}
	}
	if err := d.Work.Upsert(ctx, work); err != nil {
		return err
	}
	_, err = d.Work.AbandonSuperseded(ctx, work)
	return err
}

func (d *DeploymentWorkDeriver) now() time.Time {
	if d != nil && d.Now != nil {
		return d.Now().UTC()
	}
	return time.Now().UTC()
}

func runtimeEventKeys(events []krt.Event[RuntimeSource]) []v1alpha1store.ResourceKey {
	keys := make([]v1alpha1store.ResourceKey, 0, len(events)*2)
	for _, event := range events {
		if event.Old != nil {
			keys = append(keys, event.Old.Key)
		}
		if event.New != nil {
			keys = append(keys, event.New.Key)
		}
	}
	return keys
}

func agentEventKeys(events []krt.Event[AgentSource]) []v1alpha1store.ResourceKey {
	keys := make([]v1alpha1store.ResourceKey, 0, len(events)*2)
	for _, event := range events {
		if event.Old != nil {
			keys = append(keys, event.Old.Key)
		}
		if event.New != nil {
			keys = append(keys, event.New.Key)
		}
	}
	return keys
}

func mcpServerEventKeys(events []krt.Event[MCPServerSource]) []v1alpha1store.ResourceKey {
	keys := make([]v1alpha1store.ResourceKey, 0, len(events)*2)
	for _, event := range events {
		if event.Old != nil {
			keys = append(keys, event.Old.Key)
		}
		if event.New != nil {
			keys = append(keys, event.New.Key)
		}
	}
	return keys
}
