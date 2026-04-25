package noop

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

func TestAdapter_SatisfiesInterface(t *testing.T) {
	a := New()
	require.Equal(t, Platform, a.Platform())
	require.Contains(t, a.SupportedTargetKinds(), v1alpha1.KindAgent)
	require.Contains(t, a.SupportedTargetKinds(), v1alpha1.KindMCPServer)
}

func TestAdapter_ApplyReportsReady(t *testing.T) {
	a := New()
	dep := &v1alpha1.Deployment{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "d", Version: "v1", Generation: 3},
	}
	res, err := a.Apply(context.Background(), types.ApplyInput{Deployment: dep})
	require.NoError(t, err)
	require.NotNil(t, res)

	// Expect Ready=True with ObservedGeneration matching the input.
	var ready *v1alpha1.Condition
	for i := range res.Conditions {
		if res.Conditions[i].Type == "Ready" {
			ready = &res.Conditions[i]
			break
		}
	}
	require.NotNil(t, ready)
	require.Equal(t, v1alpha1.ConditionTrue, ready.Status)
	require.EqualValues(t, 3, ready.ObservedGeneration)

	// ProviderMetadata has the applied-at stamp.
	require.Contains(t, res.ProviderMetadata, "platforms.agentregistry.solo.io/noop/applied-at")
}

// TestAdapter_RemoveReportsRemovedCondition replaces the prior
// "drops finalizer" assertion: row lifetime is now owned by soft-delete
// + GC, so Remove only contributes the Conditions update.
func TestAdapter_RemoveReportsRemovedCondition(t *testing.T) {
	a := New()
	dep := &v1alpha1.Deployment{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "d", Version: "v1", Generation: 3},
	}
	res, err := a.Remove(context.Background(), types.RemoveInput{Deployment: dep})
	require.NoError(t, err)
	require.Equal(t, v1alpha1.ConditionFalse, res.Conditions[0].Status)
	require.Equal(t, "Removed", res.Conditions[0].Reason)
}

func TestAdapter_LogsClosesImmediately(t *testing.T) {
	a := New()
	ch, err := a.Logs(context.Background(), types.LogsInput{})
	require.NoError(t, err)
	_, ok := <-ch
	require.False(t, ok, "noop log channel should be closed on return")
}

func TestAdapter_DiscoverReturnsNothing(t *testing.T) {
	a := New()
	out, err := a.Discover(context.Background(), types.DiscoverInput{})
	require.NoError(t, err)
	require.Empty(t, out)
}
