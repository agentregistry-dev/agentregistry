package models

import (
	"encoding/json"
	"time"

	"github.com/modelcontextprotocol/registry/pkg/model"
)

// AgentJSON mirrors the ServerJSON shape for now, defined locally
type AgentJSON struct {
	AgentManifest `json:",inline"`
	Title         string             `json:"title,omitempty"`
	Version       string             `json:"version"`
	Status        string             `json:"status,omitempty"`
	WebsiteURL    string             `json:"websiteUrl,omitempty"`
	Repository    *model.Repository  `json:"repository,omitempty" doc:"Optional repository metadata for the agent source code."`
	Packages      []AgentPackageInfo `json:"packages,omitempty"`
	Remotes       []model.Transport  `json:"remotes,omitempty"`
}

var agentJSONKnownKeys = map[string]struct{}{
	"description":       {},
	"framework":         {},
	"image":             {},
	"language":          {},
	"mcpServers":        {},
	"modelName":         {},
	"modelProvider":     {},
	"name":              {},
	"packages":          {},
	"prompts":           {},
	"remotes":           {},
	"repository":        {},
	"skills":            {},
	"status":            {},
	"telemetryEndpoint": {},
	"title":             {},
	"updatedAt":         {},
	"version":           {},
	"websiteUrl":        {},
}

// MarshalJSON implements custom JSON marshaling for AgentJSON.
// It merges AdditionalElements into the top-level JSON object,
// allowing arbitrary fields to be preserved alongside known fields.
// This enables forward compatibility with extended agent manifests.
func (a AgentJSON) MarshalJSON() ([]byte, error) {
	type alias AgentJSON

	basePayload, err := json.Marshal(alias(a))
	if err != nil {
		return nil, err
	}
	if len(a.AdditionalElements) == 0 {
		return basePayload, nil
	}

	var payload map[string]any
	if err := json.Unmarshal(basePayload, &payload); err != nil {
		return nil, err
	}

	for key, value := range a.AdditionalElements {
		if _, exists := payload[key]; exists {
			continue
		}
		payload[key] = value
	}

	return json.Marshal(payload)
}

// UnmarshalJSON implements custom JSON unmarshaling for AgentJSON.
// It captures any unknown fields into AdditionalElements, supporting
// forward compatibility with extended agent manifests. This allows agents
// to include custom metadata, deployment configurations, or other
// extension fields that are preserved through the API lifecycle.
func (a *AgentJSON) UnmarshalJSON(data []byte) error {
	type alias AgentJSON

	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	extraElements := make(map[string]any)
	for key, value := range raw {
		if _, known := agentJSONKnownKeys[key]; known {
			continue
		}
		extraElements[key] = value
	}

	*a = AgentJSON(decoded)
	if len(extraElements) > 0 {
		a.AdditionalElements = extraElements
	} else {
		a.AdditionalElements = nil
	}

	return nil
}

type AgentPackageInfo struct {
	RegistryType string `json:"registryType"`
	Identifier   string `json:"identifier"`
	Version      string `json:"version"`
	Transport    struct {
		Type string `json:"type"`
	} `json:"transport"`
}

// AgentRegistryExtensions mirrors official metadata stored separately
type AgentRegistryExtensions struct {
	Status      string    `json:"status"`
	PublishedAt time.Time `json:"publishedAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	IsLatest    bool      `json:"isLatest"`
}

type AgentSemanticMeta struct {
	Score float64 `json:"score"`
}

type AgentResponseMeta struct {
	Official    *AgentRegistryExtensions `json:"io.modelcontextprotocol.registry/official,omitempty"`
	Semantic    *AgentSemanticMeta       `json:"aregistry.ai/semantic,omitempty"`
	Deployments *ResourceDeploymentsMeta `json:"aregistry.ai/deployments,omitempty"`
}

type AgentResponse struct {
	Agent         AgentJSON         `json:"agent"`
	Meta          AgentResponseMeta `json:"_meta"`
	ExtraElements map[string]any    `json:"extraElements,omitempty" doc:"Additional top-level elements preserved from the original agent payload for UI display and forward compatibility."`
}

type AgentMetadata struct {
	NextCursor string `json:"nextCursor,omitempty"`
	Count      int    `json:"count"`
}

type AgentListResponse struct {
	Agents   []AgentResponse `json:"agents"`
	Metadata AgentMetadata   `json:"metadata"`
}
