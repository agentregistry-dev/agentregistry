package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

type resultFingerprintAdapter struct {
	stubDeploymentAdapter
	result types.ApplyFingerprintResult
}

func (a resultFingerprintAdapter) DesiredFingerprintResult(context.Context, types.ApplyInput) (types.ApplyFingerprintResult, error) {
	return a.result, nil
}

type stringFingerprintAdapter struct {
	stubDeploymentAdapter
	fingerprint string
}

func (a stringFingerprintAdapter) DesiredFingerprint(context.Context, types.ApplyInput) (string, error) {
	return a.fingerprint, nil
}

type dualFingerprintAdapter struct {
	resultFingerprintAdapter
	fingerprint string
}

func (a dualFingerprintAdapter) DesiredFingerprint(context.Context, types.ApplyInput) (string, error) {
	return a.fingerprint, nil
}

func TestDesiredApplyFingerprintKeepsResultInterfaceDependencies(t *testing.T) {
	deps := []types.ApplyDependencySnapshot{{
		Kind:         v1alpha1.KindSkill,
		Namespace:    "default",
		Name:         "docs",
		Tag:          "latest",
		MaterialHash: "sha256:material",
	}}
	adapter := resultFingerprintAdapter{result: types.ApplyFingerprintResult{
		Fingerprint:  "sha256:desired",
		Dependencies: deps,
	}}

	got, err := desiredApplyFingerprint(context.Background(), adapter, types.ApplyInput{})
	require.NoError(t, err)
	require.Equal(t, "sha256:desired", got.Fingerprint)
	require.Equal(t, deps, got.Dependencies)
}

func TestDesiredApplyFingerprintPrefersResultInterfaceOverString(t *testing.T) {
	adapter := dualFingerprintAdapter{
		resultFingerprintAdapter: resultFingerprintAdapter{result: types.ApplyFingerprintResult{
			Fingerprint:  "sha256:from-result",
			Dependencies: []types.ApplyDependencySnapshot{{Kind: v1alpha1.KindPrompt, Name: "persona"}},
		}},
		fingerprint: "sha256:from-string",
	}

	got, err := desiredApplyFingerprint(context.Background(), adapter, types.ApplyInput{})
	require.NoError(t, err)
	require.Equal(t, "sha256:from-result", got.Fingerprint)
	require.Len(t, got.Dependencies, 1)
}

func TestDesiredApplyFingerprintFallsBackToStringInterface(t *testing.T) {
	adapter := stringFingerprintAdapter{fingerprint: "sha256:string-only"}

	got, err := desiredApplyFingerprint(context.Background(), adapter, types.ApplyInput{})
	require.NoError(t, err)
	require.Equal(t, "sha256:string-only", got.Fingerprint)
	require.Empty(t, got.Dependencies)
}

func TestDeploymentControllerStatusPatchPersistsDependencies(t *testing.T) {
	deployment := &v1alpha1.Deployment{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "api", Generation: 3},
	}
	deps := []types.ApplyDependencySnapshot{{
		Kind:         v1alpha1.KindSkill,
		Namespace:    "default",
		Name:         "docs",
		MaterialHash: "sha256:material",
	}}

	patch := deploymentControllerStatusPatch(deployment, nil, "sha256:desired", "force-1", deps)
	raw, err := patch(nil)
	require.NoError(t, err)

	var status v1alpha1.Status
	require.NoError(t, v1alpha1.UnmarshalStatusFromStorage(raw, &status))
	var details deploymentControllerDetails
	ok, err := status.GetDetailsKey(deploymentControllerDetailsKey, &details)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "sha256:desired", details.LastAppliedFingerprint)
	require.Equal(t, "force-1", details.LastForceToken)
	require.Equal(t, deps, details.Dependencies)
}
