package project

import (
	"fmt"

	"github.com/agentregistry-dev/agentregistry/internal/registry/kinds"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"gopkg.in/yaml.v3"
)

// loadAgentFromEnvelope decodes declarative envelope YAML and returns the
// embedded AgentManifest so the existing run/build pipelines consume it as
// before. This bridge will be removed when the unified K8s-style API refactor
// rewrites the pipelines to consume v1alpha1 types directly.
func loadAgentFromEnvelope(data []byte) (*models.AgentManifest, error) {
	var doc struct {
		Metadata kinds.Metadata  `yaml:"metadata"`
		Spec     kinds.AgentSpec `yaml:"spec"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parsing envelope agent.yaml: %w", err)
	}
	aj := kinds.ToAgentJSON(doc.Metadata, &doc.Spec)
	manifest := aj.AgentManifest
	return &manifest, nil
}
