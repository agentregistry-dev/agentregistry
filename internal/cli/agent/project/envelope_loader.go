package project

import (
	"fmt"

	agentmanifest "github.com/agentregistry-dev/agentregistry/internal/cli/agent/manifest"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// loadAgentFromEnvelope decodes declarative envelope YAML and projects it onto
// the workflow-local AgentManifest consumed by the agent runtime.
func loadAgentFromEnvelope(data []byte) (*agentmanifest.AgentManifest, error) {
	var agent v1alpha1.Agent
	if err := v1alpha1.Default.DecodeInto(data, &agent); err != nil {
		return nil, fmt.Errorf("parsing envelope agent.yaml: %w", err)
	}
	manifest := agentmanifest.FromV1Alpha1Agent(&agent)
	return &manifest, nil
}
