package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"

	"k8s.io/client-go/util/workqueue"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

const (
	DeploymentControllerFinalizer = "agentregistry.dev/deployment-controller"
	DeploymentForceAnnotation     = "reconcile.agentregistry.dev/force"

	deploymentControllerDetailsKey = "deploymentController"
)

type deploymentControllerDetails struct {
	LastAppliedFingerprint string `json:"lastAppliedFingerprint,omitempty"`
	LastForceToken         string `json:"lastForceToken,omitempty"`
}

func (c *DeploymentController) processQueueItem(
	ctx context.Context,
	queue workqueue.TypedRateLimitingInterface[deploymentQueueKey],
	key deploymentQueueKey,
) {
	defer queue.Done(key)
	outcome, message, err := c.reconcileKey(ctx, key)
	if err != nil {
		logger.Error("deployment reconcile failed", "namespace", key.Namespace, "name", key.Name, "error", err)
		queue.AddRateLimited(key)
		return
	}
	queue.Forget(key)
	if outcome != "" {
		logger.Debug("deployment reconciled", "namespace", key.Namespace, "name", key.Name, "outcome", outcome, "message", message)
	}
}

func (c *DeploymentController) reconcileKey(ctx context.Context, key deploymentQueueKey) (outcome, message string, err error) {
	deployment, found, err := c.loadDeployment(ctx, key)
	if err != nil {
		return "", "", err
	}
	if !found {
		return "missing", "deployment row no longer exists", nil
	}
	if v1alpha1.IsDiscoveredDeployment(deployment) {
		return "skipped", "discovered deployment is provider-observed state", nil
	}
	action, err := deploymentAction(deployment)
	if err != nil {
		return "", "", err
	}

	switch action {
	case ReconcileActionApply:
		return c.apply(ctx, deployment)
	case ReconcileActionDelete:
		return c.remove(ctx, deployment)
	default:
		return "", "", fmt.Errorf("unsupported deployment reconcile action %q", action)
	}
}

func (c *DeploymentController) apply(ctx context.Context, deployment *v1alpha1.Deployment) (string, string, error) {
	target, err := c.resolveTarget(ctx, deployment)
	if err != nil {
		if errors.Is(err, v1alpha1.ErrDanglingRef) {
			return c.blockReference(ctx, deployment, err)
		}
		return "", "", err
	}
	runtime, err := c.resolveRuntime(ctx, deployment)
	if err != nil {
		if errors.Is(err, v1alpha1.ErrDanglingRef) {
			return c.blockReference(ctx, deployment, err)
		}
		return "", "", err
	}
	adapter, err := c.resolveAdapter(runtime.Spec.Type)
	if err != nil {
		return "", "", err
	}
	if !adapterSupportsKind(adapter, target.GetKind()) {
		return "", "", fmt.Errorf("%w: adapter %q does not support target kind %q",
			pkgdb.ErrInvalidInput, adapter.Type(), target.GetKind())
	}
	input := types.ApplyInput{
		Deployment: deployment,
		Target:     target,
		Runtime:    runtime,
		Getter:     c.Getter,
	}
	fingerprint, err := desiredApplyFingerprint(ctx, adapter, input)
	if err != nil {
		if errors.Is(err, v1alpha1.ErrDanglingRef) {
			return c.blockReference(ctx, deployment, err)
		}
		return "", "", err
	}
	forceToken := deploymentForceToken(deployment)
	if skip, err := shouldSkipApply(deployment, fingerprint, forceToken); err != nil {
		return "", "", err
	} else if skip {
		return "unchanged", "deployment desired input unchanged", nil
	}
	result, err := adapter.Apply(ctx, input)
	if err != nil {
		if errors.Is(err, v1alpha1.ErrDanglingRef) {
			return c.blockReference(ctx, deployment, err)
		}
		return "", "", fmt.Errorf("adapter %q apply: %w", adapter.Type(), err)
	}
	if err := c.persistApplyResult(ctx, deployment, result, fingerprint, forceToken); err != nil {
		return "", "", err
	}
	return "success", "deployment applied", nil
}

func (c *DeploymentController) remove(ctx context.Context, deployment *v1alpha1.Deployment) (string, string, error) {
	runtime, err := c.resolveRuntime(ctx, deployment)
	if err != nil {
		return c.handleRemoveRuntimeError(ctx, deployment, err)
	}
	adapter, err := c.resolveAdapter(runtime.Spec.Type)
	if err != nil {
		return "", "", err
	}
	result, err := adapter.Remove(ctx, types.RemoveInput{
		Deployment: deployment,
		Runtime:    runtime,
	})
	if err != nil {
		return "", "", fmt.Errorf("adapter %q remove: %w", adapter.Type(), err)
	}
	if err := c.persistRemoveResult(ctx, deployment, result); err != nil {
		return "", "", err
	}
	if deployment.Metadata.DeletionTimestamp != nil {
		if err := c.finalizeDeletedDeployment(ctx, deployment); err != nil {
			return "", "", err
		}
	}
	return "success", "deployment removed", nil
}

func (c *DeploymentController) handleRemoveRuntimeError(
	ctx context.Context,
	deployment *v1alpha1.Deployment,
	cause error,
) (string, string, error) {
	if !errors.Is(cause, v1alpha1.ErrDanglingRef) {
		return "", "", cause
	}
	if deployment.Metadata.DeletionTimestamp == nil {
		return c.blockReference(ctx, deployment, cause)
	}
	if err := c.finalizeDeletedDeployment(ctx, deployment); err != nil {
		return "", "", err
	}
	return "success", "deployment finalized without adapter remove because runtimeRef is unavailable", nil
}

func (c *DeploymentController) blockReference(ctx context.Context, deployment *v1alpha1.Deployment, cause error) (string, string, error) {
	message := "referenced resource is not available yet"
	if cause != nil {
		message = cause.Error()
	}
	if err := c.persistApplyResult(ctx, deployment, &types.ApplyResult{
		Conditions: []v1alpha1.Condition{{
			Type:               "Ready",
			Status:             v1alpha1.ConditionFalse,
			Reason:             "ReferencePending",
			Message:            message,
			ObservedGeneration: deployment.Metadata.Generation,
		}},
	}, "", ""); err != nil {
		return "", "", err
	}
	return "blocked", message, nil
}

func (c *DeploymentController) loadDeployment(ctx context.Context, key deploymentQueueKey) (*v1alpha1.Deployment, bool, error) {
	store := c.deploymentStore()
	if store == nil {
		return nil, false, errors.New("deployment controller: no Deployment store registered")
	}
	namespace := key.Namespace
	if namespace == "" {
		namespace = v1alpha1.DefaultNamespace
	}
	raw, err := store.GetLatestIncludingTerminating(ctx, namespace, key.Name)
	if err != nil {
		if errors.Is(err, pkgdb.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	deployment, err := v1alpha1.EnvelopeFromRaw(func() *v1alpha1.Deployment { return &v1alpha1.Deployment{} }, raw, v1alpha1.KindDeployment)
	if err != nil {
		return nil, false, err
	}
	return deployment, true, nil
}

func (c *DeploymentController) resolveTarget(ctx context.Context, deployment *v1alpha1.Deployment) (v1alpha1.Object, error) {
	if c.Getter == nil {
		return nil, errors.New("deployment controller: getter is nil")
	}
	ref := deployment.Spec.TargetRef
	ref.Namespace = refNamespace(ref.Namespace, deployment.Metadata.NamespaceOrDefault())
	obj, err := c.Getter(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("resolve targetRef %s/%s@%s: %w", ref.Namespace, ref.Name, ref.Tag, err)
	}
	if obj == nil {
		return nil, fmt.Errorf("resolve targetRef %s/%s: nil object", ref.Namespace, ref.Name)
	}
	return obj, nil
}

func (c *DeploymentController) resolveRuntime(ctx context.Context, deployment *v1alpha1.Deployment) (*v1alpha1.Runtime, error) {
	if c.Getter == nil {
		return nil, errors.New("deployment controller: getter is nil")
	}
	ref := deployment.Spec.RuntimeRef
	ref.Namespace = refNamespace(ref.Namespace, deployment.Metadata.NamespaceOrDefault())
	obj, err := c.Getter(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("resolve runtimeRef %s/%s: %w", ref.Namespace, ref.Name, err)
	}
	runtime, ok := obj.(*v1alpha1.Runtime)
	if !ok || runtime == nil {
		return nil, fmt.Errorf("runtimeRef %s/%s did not resolve to a Runtime", ref.Namespace, ref.Name)
	}
	return runtime, nil
}

func (c *DeploymentController) resolveAdapter(runtimeType string) (types.DeploymentAdapter, error) {
	adapter, ok := c.Adapters[runtimeType]
	if !ok || adapter == nil {
		return nil, fmt.Errorf("deployment controller: no DeploymentAdapter registered for runtime type %q", runtimeType)
	}
	return adapter, nil
}

func (c *DeploymentController) persistApplyResult(
	ctx context.Context,
	deployment *v1alpha1.Deployment,
	result *types.ApplyResult,
	fingerprint string,
	forceToken string,
) error {
	patch := v1alpha1store.PatchOpts{
		Finalizers: ensureFinalizer(DeploymentControllerFinalizer),
	}
	if result == nil {
		if fingerprint != "" {
			patch.Status = deploymentControllerStatusPatch(deployment, nil, fingerprint, forceToken)
		}
		if err := c.deploymentStore().ApplyPatch(ctx, deployment.Metadata.NamespaceOrDefault(), deployment.Metadata.Name, "", patch); err != nil {
			return fmt.Errorf("persist apply result: %w", err)
		}
		return nil
	}
	if len(result.Conditions) > 0 || len(result.Details) > 0 || fingerprint != "" {
		patch.Status = deploymentControllerStatusPatch(deployment, result, fingerprint, forceToken)
	}
	if len(result.RuntimeMetadata) > 0 {
		patch.Annotations = func(annotations map[string]string) map[string]string {
			if annotations == nil {
				annotations = map[string]string{}
			}
			maps.Copy(annotations, result.RuntimeMetadata)
			return annotations
		}
	}
	if err := c.deploymentStore().ApplyPatch(ctx, deployment.Metadata.NamespaceOrDefault(), deployment.Metadata.Name, "", patch); err != nil {
		return fmt.Errorf("persist apply result: %w", err)
	}
	return nil
}

func deploymentControllerStatusPatch(
	deployment *v1alpha1.Deployment,
	result *types.ApplyResult,
	fingerprint string,
	forceToken string,
) func(current json.RawMessage) (json.RawMessage, error) {
	return v1alpha1.StatusPatcher(func(s *v1alpha1.Status) {
		if s.ObservedGeneration < deployment.Metadata.Generation {
			s.ObservedGeneration = deployment.Metadata.Generation
		}
		if result != nil {
			for _, cond := range result.Conditions {
				s.SetCondition(cond)
			}
			for key, encoded := range result.Details {
				_ = s.SetDetailsKeyJSON(key, encoded)
			}
		}
		if fingerprint != "" {
			_ = s.SetDetailsKey(deploymentControllerDetailsKey, deploymentControllerDetails{
				LastAppliedFingerprint: fingerprint,
				LastForceToken:         forceToken,
			})
		}
	})
}

func (c *DeploymentController) persistRemoveResult(ctx context.Context, deployment *v1alpha1.Deployment, result *types.RemoveResult) error {
	if result == nil || len(result.Conditions) == 0 {
		return nil
	}
	patch := v1alpha1store.PatchOpts{
		Status: v1alpha1.StatusPatcher(func(s *v1alpha1.Status) {
			if s.ObservedGeneration < deployment.Metadata.Generation {
				s.ObservedGeneration = deployment.Metadata.Generation
			}
			for _, cond := range result.Conditions {
				s.SetCondition(cond)
			}
		}),
	}
	if err := c.deploymentStore().ApplyPatch(ctx, deployment.Metadata.NamespaceOrDefault(), deployment.Metadata.Name, "", patch); err != nil {
		return fmt.Errorf("persist remove result: %w", err)
	}
	return nil
}

func (c *DeploymentController) finalizeDeletedDeployment(ctx context.Context, deployment *v1alpha1.Deployment) error {
	err := c.deploymentStore().PatchFinalizers(ctx, deployment.Metadata.NamespaceOrDefault(), deployment.Metadata.Name, "", removeFinalizer(DeploymentControllerFinalizer))
	if err != nil {
		if errors.Is(err, pkgdb.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("clear deployment controller finalizer: %w", err)
	}
	if _, err := c.deploymentStore().PurgeFinalized(ctx); err != nil {
		return fmt.Errorf("purge finalized deployment: %w", err)
	}
	return nil
}

func (c *DeploymentController) validateReconciler() error {
	if c == nil {
		return errors.New("deployment controller is required")
	}
	if c.deploymentStore() == nil {
		return errors.New("deployment controller: Deployment store is required")
	}
	if c.Getter == nil {
		return errors.New("deployment controller: getter is required")
	}
	if len(c.Adapters) == 0 {
		return errors.New("deployment controller: adapters are required")
	}
	return nil
}

func ensureFinalizer(finalizer string) func([]string) []string {
	return func(finalizers []string) []string {
		if slices.Contains(finalizers, finalizer) {
			return finalizers
		}
		return append(finalizers, finalizer)
	}
}

func removeFinalizer(finalizer string) func([]string) []string {
	return func(finalizers []string) []string {
		return slices.DeleteFunc(finalizers, func(existing string) bool {
			return existing == finalizer
		})
	}
}

func adapterSupportsKind(adapter types.DeploymentAdapter, kind string) bool {
	return adapter != nil && slices.Contains(adapter.SupportedTargetKinds(), kind)
}

func desiredApplyFingerprint(ctx context.Context, adapter types.DeploymentAdapter, input types.ApplyInput) (string, error) {
	if fingerprinter, ok := adapter.(types.DeploymentDesiredFingerprinter); ok {
		return fingerprinter.DesiredFingerprint(ctx, input)
	}
	adapterType := ""
	if adapter != nil {
		adapterType = adapter.Type()
	}
	return types.DefaultApplyFingerprint(ctx, input, types.ApplyFingerprintOptions{AdapterType: adapterType})
}

func shouldSkipApply(deployment *v1alpha1.Deployment, fingerprint string, forceToken string) (bool, error) {
	if deployment == nil || fingerprint == "" {
		return false, nil
	}
	var details deploymentControllerDetails
	ok, err := deployment.Status.GetDetailsKey(deploymentControllerDetailsKey, &details)
	if err != nil {
		return false, err
	}
	if !ok || details.LastAppliedFingerprint != fingerprint {
		return false, nil
	}
	return forceToken == "" || details.LastForceToken == forceToken, nil
}

func deploymentForceToken(deployment *v1alpha1.Deployment) string {
	if deployment == nil || deployment.Metadata.Annotations == nil {
		return ""
	}
	return deployment.Metadata.Annotations[DeploymentForceAnnotation]
}
