package models

import (
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
	Official *AgentRegistryExtensions `json:"io.modelcontextprotocol.registry/official,omitempty"`
	Semantic *AgentSemanticMeta       `json:"aregistry.ai/semantic,omitempty"`
}

type AgentResponse struct {
	Agent AgentJSON         `json:"agent"`
	Meta  AgentResponseMeta `json:"_meta"`
}

type AgentMetadata struct {
	NextCursor string `json:"nextCursor,omitempty"`
	Count      int    `json:"count"`
}

type AgentListResponse struct {
	Agents   []AgentResponse `json:"agents"`
	Metadata AgentMetadata   `json:"metadata"`
}
