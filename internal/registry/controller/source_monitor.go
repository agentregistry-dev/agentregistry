package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

// RunSourceMonitor periodically checks mutable deployment sources and writes
// report-only Deployment status. It intentionally does not enqueue reconciles:
// users opt into rebuilding from a newer source by changing the Deployment
// object, for example by setting DeploymentForceAnnotation.
func (c *DeploymentController) RunSourceMonitor(ctx context.Context, interval time.Duration) error {
	if c == nil {
		return errors.New("deployment source monitor: controller is required")
	}
	if interval <= 0 {
		return nil
	}
	if _, err := c.CheckDeploymentSources(ctx); err != nil {
		logger.Warn("deployment source monitor initial check failed", "error", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if _, err := c.CheckDeploymentSources(ctx); err != nil {
				logger.Warn("deployment source monitor check failed", "error", err)
			}
		}
	}
}

// CheckDeploymentSources runs one status-only source observation pass.
func (c *DeploymentController) CheckDeploymentSources(ctx context.Context) (int, error) {
	deployments, err := c.listDeployments(ctx)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, deployment := range deployments {
		if deployment == nil || deployment.Metadata.DeletionTimestamp != nil {
			continue
		}
		patched, err := c.checkDeploymentSource(ctx, deployment)
		if err != nil {
			return count, err
		}
		if patched {
			count++
		}
	}
	return count, nil
}

func (c *DeploymentController) checkDeploymentSource(ctx context.Context, deployment *v1alpha1.Deployment) (bool, error) {
	target, err := c.resolveTarget(ctx, deployment)
	if err != nil {
		if errors.Is(err, v1alpha1.ErrDanglingRef) || errors.Is(err, pkgdb.ErrNotFound) {
			return c.patchSourceRevisionStatus(ctx, deployment, nil, err, time.Now().UTC())
		}
		return false, err
	}
	runtime, err := c.resolveRuntime(ctx, deployment)
	if err != nil {
		if errors.Is(err, v1alpha1.ErrDanglingRef) || errors.Is(err, pkgdb.ErrNotFound) {
			return c.patchSourceRevisionStatus(ctx, deployment, nil, err, time.Now().UTC())
		}
		return false, err
	}
	adapter, err := c.resolveAdapter(runtime.Spec.Type)
	if err != nil {
		return c.patchSourceRevisionStatus(ctx, deployment, nil, err, time.Now().UTC())
	}
	if !adapterSupportsKind(adapter, target.GetKind()) {
		return c.patchSourceRevisionStatus(ctx, deployment, nil, fmt.Errorf("adapter %q does not support target kind %q", adapter.Type(), target.GetKind()), time.Now().UTC())
	}
	observer, ok := adapter.(types.DeploymentSourceObserver)
	if !ok {
		return false, nil
	}
	observedAt := time.Now().UTC()
	obs, err := observer.ObserveDeploymentSource(ctx, types.ApplyInput{
		Deployment: deployment,
		Target:     target,
		Runtime:    runtime,
		Getter:     c.Getter,
	})
	if obs == nil && err == nil {
		return false, nil
	}
	return c.patchSourceRevisionStatus(ctx, deployment, obs, err, observedAt)
}

func (c *DeploymentController) patchSourceRevisionStatus(
	ctx context.Context,
	deployment *v1alpha1.Deployment,
	obs *types.DeploymentSourceObservation,
	checkErr error,
	observedAt time.Time,
) (bool, error) {
	store := c.deploymentStore()
	if store == nil {
		return false, errors.New("deployment source monitor: no Deployment store registered")
	}
	namespace := deployment.Metadata.NamespaceOrDefault()
	name := deployment.Metadata.Name
	if name == "" {
		return false, nil
	}
	condition, details := sourceRevisionStatus(deployment, obs, checkErr, observedAt)
	if !shouldPatchSourceRevision(deployment.Status, condition, details) {
		return false, nil
	}
	if err := store.PatchStatus(ctx, namespace, name, "", v1alpha1.StatusPatcher(func(status *v1alpha1.Status) {
		status.SetCondition(condition)
		if err := status.SetDetailsKey(types.StatusDetailsKeySourceRevision, details); err != nil {
			logger.Warn("deployment source monitor: failed to encode source revision details", "namespace", namespace, "name", name, "error", err)
		}
	})); err != nil {
		return false, err
	}
	return true, nil
}

func sourceRevisionStatus(
	deployment *v1alpha1.Deployment,
	obs *types.DeploymentSourceObservation,
	checkErr error,
	observedAt time.Time,
) (v1alpha1.Condition, types.DeploymentSourceRevisionDetails) {
	details := types.DeploymentSourceRevisionDetails{ObservedAt: observedAt.UTC()}
	if obs != nil {
		details.Platform = obs.Platform
		details.SourceRef = obs.SourceRef
		details.AppliedRevision = obs.AppliedRevision
		details.LatestRevision = obs.LatestRevision
	}
	condition := v1alpha1.Condition{
		Type:               types.ConditionTypeSourceOutOfSync,
		Status:             v1alpha1.ConditionUnknown,
		Reason:             types.ReasonSourceRevisionPending,
		Message:            "source revision is not available yet",
		ObservedGeneration: deployment.Metadata.Generation,
	}
	if checkErr != nil {
		details.Error = checkErr.Error()
		condition.Reason = types.ReasonSourceRevisionCheckFailed
		condition.Message = checkErr.Error()
		return condition, details
	}
	if details.AppliedRevision == "" {
		condition.Message = "source revision has not been recorded for the last deploy"
		return condition, details
	}
	if details.LatestRevision == "" {
		condition.Message = "latest source revision is not available yet"
		return condition, details
	}
	if details.AppliedRevision != details.LatestRevision {
		condition.Status = v1alpha1.ConditionTrue
		condition.Reason = types.ReasonSourceRevisionChanged
		condition.Message = "latest source revision differs from the last deployed revision"
		return condition, details
	}
	condition.Status = v1alpha1.ConditionFalse
	condition.Reason = types.ReasonSourceRevisionAligned
	condition.Message = "deployed source revision matches the latest observed revision"
	return condition, details
}

func shouldPatchSourceRevision(current v1alpha1.Status, next v1alpha1.Condition, details types.DeploymentSourceRevisionDetails) bool {
	existing := current.GetCondition(types.ConditionTypeSourceOutOfSync)
	if existing == nil ||
		existing.Status != next.Status ||
		existing.Reason != next.Reason ||
		existing.Message != next.Message ||
		existing.ObservedGeneration != next.ObservedGeneration {
		return true
	}
	var existingDetails types.DeploymentSourceRevisionDetails
	ok, err := current.GetDetailsKey(types.StatusDetailsKeySourceRevision, &existingDetails)
	if err != nil || !ok {
		return true
	}
	return existingDetails.Platform != details.Platform ||
		existingDetails.SourceRef != details.SourceRef ||
		existingDetails.AppliedRevision != details.AppliedRevision ||
		existingDetails.LatestRevision != details.LatestRevision ||
		existingDetails.Error != details.Error
}
