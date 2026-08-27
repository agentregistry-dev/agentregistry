package kagent

import (
	"context"
	"fmt"

	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

func (a *adapter) Discover(ctx context.Context, in types.DiscoverInput) ([]types.DiscoveryResult, error) {
	if in.Runtime == nil {
		return nil, fmt.Errorf("kagent discover: runtime is required")
	}
	rcfg, err := decodeRuntimeConfig(in.Runtime.Spec.Config)
	if err != nil {
		return nil, fmt.Errorf("kagent discover: %w", err)
	}
	client, err := a.clientFor(ctx, in.Runtime, rcfg)
	if err != nil {
		return nil, fmt.Errorf("build kagent client: %w", err)
	}
	agents, err := client.listAgents(ctx)
	if err != nil {
		return nil, err
	}
	servers, err := client.listToolServers(ctx)
	if err != nil {
		return nil, err
	}
	ns := defaultNamespace(rcfg.Namespace, defaultRuntimeNamespace)
	workloads := append(agents, servers...)
	out := make([]types.DiscoveryResult, 0, len(workloads))
	for _, w := range workloads {
		if w.Namespace != ns {
			continue
		}
		out = append(out, types.DiscoveryResult{
			TargetKind: w.Kind,
			Name:       w.Name,
			RuntimeMetadata: map[string]string{
				types.RuntimeMetadataRemoteIDKey: w.Name,
				"namespace":                      w.Namespace,
			},
		})
	}
	return out, nil
}
