package controller

import (
	"errors"
	"fmt"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// ReconcileAction is the operation implied by a Deployment's current source
// state.
type ReconcileAction string

const (
	// ReconcileActionApply converges a Deployment toward running desired state.
	ReconcileActionApply ReconcileAction = "apply"
	// ReconcileActionDelete tears down runtime resources for an undeploy/delete.
	ReconcileActionDelete ReconcileAction = "delete"
)

type deploymentQueueKey struct {
	Namespace string
	Name      string
}

func deploymentAction(deployment *v1alpha1.Deployment) (ReconcileAction, error) {
	if deployment == nil {
		return "", errors.New("controller: deployment is required")
	}
	if deployment.Metadata.DeletionTimestamp != nil {
		return ReconcileActionDelete, nil
	}
	switch deployment.Spec.DesiredState {
	case "", v1alpha1.DesiredStateDeployed:
		return ReconcileActionApply, nil
	case v1alpha1.DesiredStateUndeployed:
		return ReconcileActionDelete, nil
	default:
		return "", fmt.Errorf("controller: unsupported deployment desiredState %q", deployment.Spec.DesiredState)
	}
}
