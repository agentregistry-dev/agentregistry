package controller

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

const (
	DeploymentControllerFinalizer = "agentregistry.dev/deployment-controller"

	defaultExecutorOwner = "deployment-controller"
	// Keep the default lease long enough for external adapter calls; without
	// lease renewal, an expired claim may be retried by another worker.
	defaultExecutorLeaseDuration = 5 * time.Minute
	defaultExecutorBackoff       = 30 * time.Second
)

// DeploymentExecutor owns Deployment adapter side effects. It only acts after
// durable reconcile_work has been claimed with a lease token.
type DeploymentExecutor struct {
	Stores   map[string]*v1alpha1store.Store
	Adapters map[string]types.DeploymentAdapter
	Getter   v1alpha1.GetterFunc

	Work   *v1alpha1store.ReconcileWorkStore
	Events *v1alpha1store.ReconcileEventStore

	Owner         string
	LeaseDuration time.Duration
	BackoffDelay  time.Duration
	Now           func() time.Time
}

func (e *DeploymentExecutor) Run(ctx context.Context, interval time.Duration, limit int) error {
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		if _, err := e.RunOnce(ctx, limit); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (e *DeploymentExecutor) RunOnce(ctx context.Context, limit int) (int, error) {
	if err := e.validate(); err != nil {
		return 0, err
	}
	leaseUntil := e.now().Add(e.leaseDuration())
	claims, err := e.Work.ClaimDue(ctx, e.owner(), leaseUntil, limit)
	if err != nil {
		return 0, err
	}
	for i, work := range claims {
		if work.Resource.Kind != v1alpha1.KindDeployment {
			continue
		}
		if err := e.executeClaim(ctx, work); err != nil {
			return i, err
		}
	}
	return len(claims), nil
}

func (e *DeploymentExecutor) executeClaim(ctx context.Context, work v1alpha1store.ReconcileWork) error {
	outcome, message, reconcileErr := e.reconcile(ctx, work)
	if reconcileErr != nil {
		next := e.now().Add(e.backoffDelay())
		if _, err := e.Events.Append(ctx, v1alpha1store.ReconcileEvent{
			WorkKey:       work.Key,
			Resource:      work.Resource,
			UID:           work.UID,
			Generation:    work.Generation,
			Action:        work.Action,
			Attempt:       work.Attempt,
			Outcome:       "error",
			Message:       message,
			Error:         reconcileErr.Error(),
			NextAttemptAt: &next,
		}); err != nil {
			return err
		}
		_, err := e.Work.Backoff(ctx, work.Key, work.LeaseToken, reconcileErr.Error(), next)
		return err
	}

	if _, err := e.Events.Append(ctx, v1alpha1store.ReconcileEvent{
		WorkKey:    work.Key,
		Resource:   work.Resource,
		UID:        work.UID,
		Generation: work.Generation,
		Action:     work.Action,
		Attempt:    work.Attempt,
		Outcome:    outcome,
		Message:    message,
	}); err != nil {
		return err
	}
	_, err := e.Work.Complete(ctx, work.Key, work.LeaseToken)
	return err
}

func (e *DeploymentExecutor) reconcile(ctx context.Context, work v1alpha1store.ReconcileWork) (outcome, message string, err error) {
	deployment, found, err := e.loadDeployment(ctx, work)
	if err != nil {
		return "", "", err
	}
	if !found {
		return "stale", "deployment row no longer exists", nil
	}
	if deployment.Metadata.UID != work.UID || deployment.Metadata.Generation != work.Generation {
		return "stale", "deployment uid or generation changed before execution", nil
	}
	if work.Action == ReconcileActionApply && deployment.Metadata.DeletionTimestamp != nil {
		return "stale", "deployment is terminating; skipping apply", nil
	}

	switch work.Action {
	case ReconcileActionApply:
		return e.apply(ctx, deployment)
	case ReconcileActionRemove:
		return e.remove(ctx, deployment)
	default:
		return "", "", fmt.Errorf("unsupported deployment reconcile action %q", work.Action)
	}
}

func (e *DeploymentExecutor) apply(ctx context.Context, deployment *v1alpha1.Deployment) (string, string, error) {
	target, err := e.resolveTarget(ctx, deployment)
	if err != nil {
		if errors.Is(err, v1alpha1.ErrDanglingRef) {
			return e.blockReference(ctx, deployment, err)
		}
		return "", "", err
	}
	runtime, err := e.resolveRuntime(ctx, deployment)
	if err != nil {
		if errors.Is(err, v1alpha1.ErrDanglingRef) {
			return e.blockReference(ctx, deployment, err)
		}
		return "", "", err
	}
	adapter, err := e.resolveAdapter(runtime.Spec.Type)
	if err != nil {
		return "", "", err
	}
	if !adapterSupportsKind(adapter, target.GetKind()) {
		return "", "", fmt.Errorf("%w: adapter %q does not support target kind %q",
			pkgdb.ErrInvalidInput, adapter.Type(), target.GetKind())
	}
	result, err := adapter.Apply(ctx, types.ApplyInput{
		Deployment: deployment,
		Target:     target,
		Runtime:    runtime,
		Getter:     e.Getter,
	})
	if err != nil {
		return "", "", fmt.Errorf("adapter %q apply: %w", adapter.Type(), err)
	}
	if err := e.persistApplyResult(ctx, deployment, result); err != nil {
		return "", "", err
	}
	return "success", "deployment applied", nil
}

func (e *DeploymentExecutor) remove(ctx context.Context, deployment *v1alpha1.Deployment) (string, string, error) {
	runtime, err := e.resolveRuntime(ctx, deployment)
	if err != nil {
		return e.handleRemoveRuntimeError(ctx, deployment, err)
	}
	adapter, err := e.resolveAdapter(runtime.Spec.Type)
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
	if err := e.persistRemoveResult(ctx, deployment, result); err != nil {
		return "", "", err
	}
	if deployment.Metadata.DeletionTimestamp != nil {
		if err := e.finalizeDeletedDeployment(ctx, deployment); err != nil {
			return "", "", err
		}
	}
	return "success", "deployment removed", nil
}

func (e *DeploymentExecutor) handleRemoveRuntimeError(
	ctx context.Context,
	deployment *v1alpha1.Deployment,
	cause error,
) (string, string, error) {
	if !errors.Is(cause, v1alpha1.ErrDanglingRef) {
		return "", "", cause
	}
	if deployment.Metadata.DeletionTimestamp == nil {
		return e.blockReference(ctx, deployment, cause)
	}
	if err := e.finalizeDeletedDeployment(ctx, deployment); err != nil {
		return "", "", err
	}
	return "success", "deployment finalized without adapter remove because runtimeRef is unavailable", nil
}

func (e *DeploymentExecutor) blockReference(ctx context.Context, deployment *v1alpha1.Deployment, cause error) (string, string, error) {
	message := "referenced resource is not available yet"
	if cause != nil {
		message = cause.Error()
	}
	if err := e.persistApplyResult(ctx, deployment, &types.ApplyResult{
		Conditions: []v1alpha1.Condition{{
			Type:               "Ready",
			Status:             v1alpha1.ConditionFalse,
			Reason:             "ReferencePending",
			Message:            message,
			ObservedGeneration: deployment.Metadata.Generation,
		}},
	}); err != nil {
		return "", "", err
	}
	return "blocked", message, nil
}

func (e *DeploymentExecutor) loadDeployment(ctx context.Context, work v1alpha1store.ReconcileWork) (*v1alpha1.Deployment, bool, error) {
	store := e.deploymentStore()
	if store == nil {
		return nil, false, errors.New("deployment executor: no Deployment store registered")
	}
	raw, err := store.GetLatestIncludingTerminating(ctx, work.Resource.Namespace, work.Resource.Name)
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

func (e *DeploymentExecutor) resolveTarget(ctx context.Context, deployment *v1alpha1.Deployment) (v1alpha1.Object, error) {
	if e.Getter == nil {
		return nil, errors.New("deployment executor: getter is nil")
	}
	ref := deployment.Spec.TargetRef
	ref.Namespace = refNamespace(ref.Namespace, deployment.Metadata.NamespaceOrDefault())
	obj, err := e.Getter(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("resolve targetRef %s/%s@%s: %w", ref.Namespace, ref.Name, ref.Tag, err)
	}
	if obj == nil {
		return nil, fmt.Errorf("resolve targetRef %s/%s: nil object", ref.Namespace, ref.Name)
	}
	return obj, nil
}

func (e *DeploymentExecutor) resolveRuntime(ctx context.Context, deployment *v1alpha1.Deployment) (*v1alpha1.Runtime, error) {
	if e.Getter == nil {
		return nil, errors.New("deployment executor: getter is nil")
	}
	ref := deployment.Spec.RuntimeRef
	ref.Namespace = refNamespace(ref.Namespace, deployment.Metadata.NamespaceOrDefault())
	obj, err := e.Getter(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("resolve runtimeRef %s/%s: %w", ref.Namespace, ref.Name, err)
	}
	runtime, ok := obj.(*v1alpha1.Runtime)
	if !ok || runtime == nil {
		return nil, fmt.Errorf("runtimeRef %s/%s did not resolve to a Runtime", ref.Namespace, ref.Name)
	}
	return runtime, nil
}

func (e *DeploymentExecutor) resolveAdapter(runtimeType string) (types.DeploymentAdapter, error) {
	adapter, ok := e.Adapters[runtimeType]
	if !ok || adapter == nil {
		return nil, fmt.Errorf("deployment executor: no DeploymentAdapter registered for runtime type %q", runtimeType)
	}
	return adapter, nil
}

func (e *DeploymentExecutor) persistApplyResult(ctx context.Context, deployment *v1alpha1.Deployment, result *types.ApplyResult) error {
	patch := v1alpha1store.PatchOpts{
		Finalizers: ensureFinalizer(DeploymentControllerFinalizer),
	}
	if result == nil {
		if err := e.deploymentStore().ApplyPatch(ctx, deployment.Metadata.NamespaceOrDefault(), deployment.Metadata.Name, "", patch); err != nil {
			return fmt.Errorf("persist apply result: %w", err)
		}
		return nil
	}
	if len(result.Conditions) > 0 || len(result.Details) > 0 {
		patch.Status = v1alpha1.StatusPatcher(func(s *v1alpha1.Status) {
			if s.ObservedGeneration < deployment.Metadata.Generation {
				s.ObservedGeneration = deployment.Metadata.Generation
			}
			for _, cond := range result.Conditions {
				s.SetCondition(cond)
			}
			for key, encoded := range result.Details {
				_ = s.SetDetailsKeyJSON(key, encoded)
			}
		})
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
	if err := e.deploymentStore().ApplyPatch(ctx, deployment.Metadata.NamespaceOrDefault(), deployment.Metadata.Name, "", patch); err != nil {
		return fmt.Errorf("persist apply result: %w", err)
	}
	return nil
}

func (e *DeploymentExecutor) persistRemoveResult(ctx context.Context, deployment *v1alpha1.Deployment, result *types.RemoveResult) error {
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
	if err := e.deploymentStore().ApplyPatch(ctx, deployment.Metadata.NamespaceOrDefault(), deployment.Metadata.Name, "", patch); err != nil {
		return fmt.Errorf("persist remove result: %w", err)
	}
	return nil
}

func (e *DeploymentExecutor) finalizeDeletedDeployment(ctx context.Context, deployment *v1alpha1.Deployment) error {
	err := e.deploymentStore().PatchFinalizers(ctx, deployment.Metadata.NamespaceOrDefault(), deployment.Metadata.Name, "", removeFinalizer(DeploymentControllerFinalizer))
	if err != nil {
		if errors.Is(err, pkgdb.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("clear deployment controller finalizer: %w", err)
	}
	if _, err := e.deploymentStore().PurgeFinalized(ctx); err != nil {
		return fmt.Errorf("purge finalized deployment: %w", err)
	}
	return nil
}

func (e *DeploymentExecutor) deploymentStore() *v1alpha1store.Store {
	if e == nil || e.Stores == nil {
		return nil
	}
	return e.Stores[v1alpha1.KindDeployment]
}

func (e *DeploymentExecutor) validate() error {
	if e == nil {
		return errors.New("deployment executor is required")
	}
	if e.Work == nil {
		return errors.New("deployment executor: work store is required")
	}
	if e.Events == nil {
		return errors.New("deployment executor: event store is required")
	}
	if e.deploymentStore() == nil {
		return errors.New("deployment executor: Deployment store is required")
	}
	return nil
}

func (e *DeploymentExecutor) owner() string {
	if e != nil && e.Owner != "" {
		return e.Owner
	}
	return defaultExecutorOwner
}

func (e *DeploymentExecutor) leaseDuration() time.Duration {
	if e != nil && e.LeaseDuration > 0 {
		return e.LeaseDuration
	}
	return defaultExecutorLeaseDuration
}

func (e *DeploymentExecutor) backoffDelay() time.Duration {
	if e != nil && e.BackoffDelay > 0 {
		return e.BackoffDelay
	}
	return defaultExecutorBackoff
}

func (e *DeploymentExecutor) now() time.Time {
	if e != nil && e.Now != nil {
		return e.Now().UTC()
	}
	return time.Now().UTC()
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
