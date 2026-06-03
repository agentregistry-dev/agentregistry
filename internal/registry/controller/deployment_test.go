package controller

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

func TestDeploymentActionDefaultsToApply(t *testing.T) {
	action, err := deploymentAction(deploymentFixture(""))
	require.NoError(t, err)

	require.Equal(t, ReconcileActionApply, action)
}

func TestDeploymentActionDeletesForDesiredUndeployed(t *testing.T) {
	action, err := deploymentAction(deploymentFixture(v1alpha1.DesiredStateUndeployed))
	require.NoError(t, err)

	require.Equal(t, ReconcileActionDelete, action)
}

func TestDeploymentActionDeletesForTerminatingDeployment(t *testing.T) {
	deployment := deploymentFixture(v1alpha1.DesiredStateDeployed)
	now := time.Now()
	deployment.Metadata.DeletionTimestamp = &now

	action, err := deploymentAction(deployment)
	require.NoError(t, err)
	require.Equal(t, ReconcileActionDelete, action)
}

func TestDeploymentActionRejectsInvalidDesiredState(t *testing.T) {
	_, err := deploymentAction(deploymentFixture("running"))
	require.ErrorContains(t, err, "unsupported deployment desiredState")
}

func deploymentFixture(desiredState string) *v1alpha1.Deployment {
	return &v1alpha1.Deployment{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindDeployment},
		Metadata: v1alpha1.ObjectMeta{
			Namespace:  v1alpha1.DefaultNamespace,
			Name:       "weather",
			UID:        "uid-1",
			Generation: 7,
		},
		Spec: v1alpha1.DeploymentSpec{
			TargetRef:    v1alpha1.ResourceRef{Kind: v1alpha1.KindMCPServer, Name: "weather", Tag: "stable"},
			RuntimeRef:   v1alpha1.ResourceRef{Kind: v1alpha1.KindRuntime, Name: "local"},
			DesiredState: desiredState,
		},
	}
}
