// Package noop provides a reference DeploymentAdapter that satisfies the
// v1alpha1 interface without actually touching infrastructure. It's
// intended for:
//   - integration tests that exercise the reconciler lifecycle without
//     needing docker-compose or a kubernetes cluster
//   - a placeholder Platform="noop" Provider entry while
//     local+kubernetes native ports are in progress
//   - a baseline for contributors implementing new adapters — demonstrates
//     the expected Apply/Remove/Logs/Discover shape end-to-end
//
// Apply immediately writes Conditions = {Ready=True, Reason="NoopComplete"}
// and a Removed=False annotation timestamp. Remove flips Ready=False with
// Reason="NoopRemoved". Row lifetime is governed by the soft-delete + GC
// path; the adapter contributes no finalizer tokens.
package noop

import (
	"context"
	"time"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

const Platform = "noop"

// Adapter implements types.DeploymentAdapter with no side effects beyond
// status reporting. Safe to register in test harnesses; intentionally
// not registered in registry_app.App.
type Adapter struct{}

// New returns a ready-to-use Adapter.
func New() *Adapter { return &Adapter{} }

// Platform returns "noop".
func (a *Adapter) Platform() string { return Platform }

// SupportedTargetKinds returns both Agent and MCPServer — the noop
// adapter declares broad support since it does nothing anyway.
func (a *Adapter) SupportedTargetKinds() []string {
	return []string{v1alpha1.KindAgent, v1alpha1.KindMCPServer}
}

// Apply reports synthetic convergence immediately.
func (a *Adapter) Apply(ctx context.Context, in types.ApplyInput) (*types.ApplyResult, error) {
	now := time.Now().UTC()
	return &types.ApplyResult{
		Conditions: []v1alpha1.Condition{
			{
				Type:               "Ready",
				Status:             v1alpha1.ConditionTrue,
				Reason:             "NoopComplete",
				Message:            "noop adapter — no real workload was started",
				LastTransitionTime: now,
				ObservedGeneration: in.Deployment.Metadata.Generation,
			},
			{
				Type:               "ProviderConfigured",
				Status:             v1alpha1.ConditionTrue,
				Reason:             "NoopProvider",
				Message:            "noop platform requires no provider configuration",
				LastTransitionTime: now,
				ObservedGeneration: in.Deployment.Metadata.Generation,
			},
		},
		ProviderMetadata: map[string]string{
			"platforms.agentregistry.solo.io/noop/applied-at": now.Format(time.RFC3339),
		},
	}, nil
}

// Remove reports Ready=False once teardown completes; row lifetime is
// owned by the soft-delete + GC path, not by adapter-returned tokens.
func (a *Adapter) Remove(ctx context.Context, in types.RemoveInput) (*types.RemoveResult, error) {
	now := time.Now().UTC()
	return &types.RemoveResult{
		Conditions: []v1alpha1.Condition{
			{
				Type:               "Ready",
				Status:             v1alpha1.ConditionFalse,
				Reason:             "Removed",
				Message:            "noop adapter — teardown complete",
				LastTransitionTime: now,
				ObservedGeneration: in.Deployment.Metadata.Generation,
			},
		},
	}, nil
}

// Logs returns an immediately-closed channel — no logs to stream.
func (a *Adapter) Logs(ctx context.Context, in types.LogsInput) (<-chan types.LogLine, error) {
	ch := make(chan types.LogLine)
	close(ch)
	return ch, nil
}

// Discover reports nothing — noop has no out-of-band workloads.
func (a *Adapter) Discover(ctx context.Context, in types.DiscoverInput) ([]types.DiscoveryResult, error) {
	return nil, nil
}

// Compile-time assertion that Adapter satisfies DeploymentAdapter.
var _ types.DeploymentAdapter = (*Adapter)(nil)
