package controller

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

func TestDeriveDeploymentWorkDefaultsToApply(t *testing.T) {
	work, err := DeriveDeploymentWork(deploymentFixture(""))
	require.NoError(t, err)

	require.Equal(t, "Deployment:default:weather:uid-1:7:apply", work.Key)
	require.Equal(t, string(ReconcileActionApply), work.Action)
	require.Equal(t, "desired-deployed", work.Reason)
	require.Equal(t, int64(7), work.Generation)
}

func TestDeriveDeploymentWorkDeletesForDesiredUndeployed(t *testing.T) {
	work, err := DeriveDeploymentWork(deploymentFixture(v1alpha1.DesiredStateUndeployed))
	require.NoError(t, err)

	require.Equal(t, string(ReconcileActionDelete), work.Action)
	require.Equal(t, "desired-undeployed", work.Reason)
	require.Equal(t, "Deployment:default:weather:uid-1:7:delete", work.Key)
}

func TestDeriveDeploymentWorkDeletesForTerminatingDeployment(t *testing.T) {
	deployment := deploymentFixture(v1alpha1.DesiredStateDeployed)
	now := time.Now()
	deployment.Metadata.DeletionTimestamp = &now

	work, err := DeriveDeploymentWork(deployment)
	require.NoError(t, err)
	require.Equal(t, string(ReconcileActionDelete), work.Action)
	require.Equal(t, "terminating", work.Reason)
}

func TestDeriveDeploymentWorkRejectsInvalidDesiredState(t *testing.T) {
	_, err := DeriveDeploymentWork(deploymentFixture("running"))
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
