package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
)

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
	if v1alpha1.IsDiscoveredDeployment(deployment.Deployment) {
		return "skipped", "discovered deployment is provider-observed state", nil
	}
	action, err := deploymentAction(deployment.Deployment)
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

func (c *DeploymentController) apply(ctx context.Context, deployment *types.DeploymentRecord) (string, string, error) {
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
	fingerprintResult, err := desiredApplyFingerprint(ctx, adapter, input)
	if err != nil {
		if errors.Is(err, v1alpha1.ErrDanglingRef) {
			return c.blockReference(ctx, deployment, err)
		}
		return "", "", err
	}
	fingerprint := fingerprintResult.Fingerprint
	forceToken := deploymentForceToken(deployment)
	if skip, err := shouldSkipApply(deployment, fingerprint, forceToken); err != nil {
		return "", "", err
	} else if skip {
		return "unchanged", "deployment desired input unchanged", nil
	}
	result, err := adapter.Apply(ctx, input)
	if err != nil {
		if c.DeploymentApplied != nil {
			c.DeploymentApplied(ctx, input, nil, err)
		}
		if errors.Is(err, v1alpha1.ErrDanglingRef) {
			return c.blockReference(ctx, deployment, err)
		}
		return "", "", fmt.Errorf("adapter %q apply: %w", adapter.Type(), err)
	}
	if err := c.persistApplyResult(ctx, deployment, result, fingerprint, forceToken); err != nil {
		if c.DeploymentApplied != nil {
			c.DeploymentApplied(ctx, input, result, err)
		}
		return "", "", err
	}
	if c.DeploymentApplied != nil {
		c.DeploymentApplied(ctx, input, result, nil)
	}
	return "success", "deployment applied", nil
}

func (c *DeploymentController) remove(ctx context.Context, deployment *types.DeploymentRecord) (string, string, error) {
	runtime, err := c.resolveRuntimeIncludingTerminating(ctx, deployment)
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

// resolveRuntimeIncludingTerminating keeps the parent Runtime available after
// its deletion is accepted. Deployment removal still needs that Runtime to
// select and configure the provider responsible for child cleanup; filtering
// terminating rows would orphan the provider resources. A truly absent parent
// falls through to the existing dangling-reference finalization path.
func (c *DeploymentController) resolveRuntimeIncludingTerminating(
	ctx context.Context,
	deployment *types.DeploymentRecord,
) (*v1alpha1.Runtime, error) {
	store := c.Stores[v1alpha1.KindRuntime]
	if store == nil {
		return nil, errors.New("deployment controller: no Runtime store registered")
	}
	ref := deployment.Spec.RuntimeRef
	ref.Namespace = refNamespace(ref.Namespace, deployment.Metadata.NamespaceOrDefault())
	raw, err := store.GetLatestIncludingTerminating(ctx, ref.Namespace, ref.Name)
	if err != nil {
		if errors.Is(err, pkgdb.ErrNotFound) {
			return nil, v1alpha1.ErrDanglingRef
		}
		return nil, fmt.Errorf("resolve runtimeRef %s/%s: %w", ref.Namespace, ref.Name, err)
	}
	runtime, err := v1alpha1.EnvelopeFromRaw(func() *v1alpha1.Runtime {
		return &v1alpha1.Runtime{}
	}, raw, v1alpha1.KindRuntime)
	if err != nil {
		return nil, fmt.Errorf("resolve runtimeRef %s/%s: %w", ref.Namespace, ref.Name, err)
	}
	return runtime, nil
}

func (c *DeploymentController) handleRemoveRuntimeError(
	ctx context.Context,
	deployment *types.DeploymentRecord,
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

func (c *DeploymentController) blockReference(ctx context.Context, deployment *types.DeploymentRecord, cause error) (string, string, error) {
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

func (c *DeploymentController) loadDeployment(ctx context.Context, key deploymentQueueKey) (*types.DeploymentRecord, bool, error) {
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
	deployment, err := types.DeploymentRecordFromRaw(raw)
	if err != nil {
		return nil, false, err
	}
	return deployment, true, nil
}

func (c *DeploymentController) resolveTarget(ctx context.Context, deployment *types.DeploymentRecord) (v1alpha1.Object, error) {
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

func (c *DeploymentController) resolveRuntime(ctx context.Context, deployment *types.DeploymentRecord) (*v1alpha1.Runtime, error) {
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
	deployment *types.DeploymentRecord,
	result *types.ApplyResult,
	fingerprint string,
	forceToken string,
) error {
	store := c.deploymentStore()
	namespace, name := deployment.Metadata.NamespaceOrDefault(), deployment.Metadata.Name
	if err := store.PatchFinalizers(ctx, namespace, name, "", ensureFinalizer(DeploymentControllerFinalizer)); err != nil {
		return fmt.Errorf("persist apply finalizer: %w", err)
	}

	meta := deployment.InternalMeta
	if result != nil && result.InternalMeta != nil {
		meta = *result.InternalMeta
	}
	if fingerprint != "" {
		meta.LastAppliedFingerprint = fingerprint
		meta.LastForceToken = forceToken
	}
	if result == nil && fingerprint == "" {
		return nil
	}
	if err := store.PatchStatusAndMeta(ctx, namespace, name, deploymentStatusPatch(deployment, result), meta); err != nil {
		return fmt.Errorf("persist apply result: %w", err)
	}
	return nil
}

func deploymentStatusPatch(deployment *types.DeploymentRecord, result *types.ApplyResult) func(current json.RawMessage) (json.RawMessage, error) {
	return v1alpha1.DeploymentStatusPatcher(func(s *v1alpha1.DeploymentStatus) {
		if s.ObservedGeneration < deployment.Metadata.Generation {
			s.ObservedGeneration = deployment.Metadata.Generation
		}
		if result != nil {
			if result.Runtime != nil {
				s.Runtime = result.Runtime
			}
			for _, cond := range result.Conditions {
				s.SetCondition(cond)
			}
		}
	})
}

func (c *DeploymentController) persistRemoveResult(ctx context.Context, deployment *types.DeploymentRecord, result *types.RemoveResult) error {
	if result == nil || len(result.Conditions) == 0 {
		return nil
	}
	patch := v1alpha1store.PatchOpts{
		Status: v1alpha1.DeploymentStatusPatcher(func(s *v1alpha1.DeploymentStatus) {
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

func (c *DeploymentController) finalizeDeletedDeployment(ctx context.Context, deployment *types.DeploymentRecord) error {
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
	if c.DeploymentFinalized != nil {
		c.DeploymentFinalized(ctx, deployment.Deployment)
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

type desiredApplyFingerprintResult struct {
	Fingerprint string
}

func desiredApplyFingerprint(ctx context.Context, adapter types.DeploymentAdapter, input types.ApplyInput) (desiredApplyFingerprintResult, error) {
	if fingerprinter, ok := adapter.(types.DeploymentDesiredFingerprinter); ok {
		fingerprint, err := fingerprinter.DesiredFingerprint(ctx, input)
		return desiredApplyFingerprintResult{Fingerprint: fingerprint}, err
	}
	adapterType := ""
	if adapter != nil {
		adapterType = adapter.Type()
	}
	result, err := types.DefaultApplyFingerprintResult(ctx, input, types.ApplyFingerprintOptions{AdapterType: adapterType})
	if err != nil {
		return desiredApplyFingerprintResult{}, err
	}
	return desiredApplyFingerprintResult{Fingerprint: result.Fingerprint}, nil
}

func shouldSkipApply(deployment *types.DeploymentRecord, fingerprint string, forceToken string) (bool, error) {
	if deployment == nil || fingerprint == "" {
		return false, nil
	}
	if deployment.InternalMeta.LastAppliedFingerprint != fingerprint {
		return false, nil
	}
	return forceToken == "" || deployment.InternalMeta.LastForceToken == forceToken, nil
}

func deploymentForceToken(deployment *types.DeploymentRecord) string {
	if deployment == nil || deployment.Metadata.Annotations == nil {
		return ""
	}
	return deployment.Metadata.Annotations[DeploymentForceAnnotation]
}
