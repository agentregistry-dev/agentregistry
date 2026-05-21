package controller

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

const (
	// ReconcileActionApply converges a Deployment toward running desired state.
	ReconcileActionApply = "apply"
	// ReconcileActionRemove tears down runtime resources.
	ReconcileActionRemove = "remove"
)

// DeploymentRequestPayload is the payload persisted with Deployment
// reconcile_work. It captures refs needed by a future executor without
// embedding full object payloads.
type DeploymentRequestPayload struct {
	TargetRef    v1alpha1.ResourceRef `json:"targetRef"`
	RuntimeRef   v1alpha1.ResourceRef `json:"runtimeRef"`
	DesiredState string               `json:"desiredState,omitempty"`
}

// DeriveDeploymentWork converts a Deployment source row into durable work. It
// performs no adapter calls and does not resolve references; executors must
// re-read the row and dependencies after claiming.
func DeriveDeploymentWork(deployment *v1alpha1.Deployment) (v1alpha1store.ReconcileWork, error) {
	if deployment == nil {
		return v1alpha1store.ReconcileWork{}, errors.New("controller: deployment is required")
	}
	meta := deployment.Metadata
	namespace := meta.NamespaceOrDefault()
	if meta.Name == "" {
		return v1alpha1store.ReconcileWork{}, errors.New("controller: deployment metadata.name is required")
	}
	if meta.Generation <= 0 {
		return v1alpha1store.ReconcileWork{}, errors.New("controller: deployment metadata.generation must be positive")
	}

	action, reason, err := deploymentAction(deployment)
	if err != nil {
		return v1alpha1store.ReconcileWork{}, err
	}
	payload, err := json.Marshal(DeploymentRequestPayload{
		TargetRef:    deployment.Spec.TargetRef,
		RuntimeRef:   deployment.Spec.RuntimeRef,
		DesiredState: deployment.Spec.DesiredState,
	})
	if err != nil {
		return v1alpha1store.ReconcileWork{}, fmt.Errorf("controller: marshal deployment request payload: %w", err)
	}

	resource := v1alpha1store.ResourceKey{
		Kind:      v1alpha1.KindDeployment,
		Namespace: namespace,
		Name:      meta.Name,
	}
	return v1alpha1store.ReconcileWork{
		Key:        deploymentWorkKey(resource, meta.UID, meta.Generation, action),
		Resource:   resource,
		UID:        meta.UID,
		Generation: meta.Generation,
		Action:     action,
		Reason:     reason,
		Payload:    payload,
	}, nil
}

func deploymentAction(deployment *v1alpha1.Deployment) (action, reason string, err error) {
	if deployment.Metadata.DeletionTimestamp != nil {
		return ReconcileActionRemove, "terminating", nil
	}
	switch deployment.Spec.DesiredState {
	case "", v1alpha1.DesiredStateDeployed:
		return ReconcileActionApply, "desired-deployed", nil
	case v1alpha1.DesiredStateUndeployed:
		return ReconcileActionRemove, "desired-undeployed", nil
	default:
		return "", "", fmt.Errorf("controller: unsupported deployment desiredState %q", deployment.Spec.DesiredState)
	}
}

func deploymentWorkKey(resource v1alpha1store.ResourceKey, uid string, generation int64, action string) string {
	return fmt.Sprintf("%s:%s:%s:%s:%d:%s", resource.Kind, resource.Namespace, resource.Name, uid, generation, action)
}
