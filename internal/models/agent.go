package models

import (
	"time"

	"github.com/kagent-dev/kagent/go/cli/agent/frameworks/common"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

// AgentJSON mirrors the ServerJSON shape for now, defined locally
type AgentJSON struct {
	common.AgentManifest `json:",inline"`
	Title                string `json:"title,omitempty"`
	Version              string `json:"version"`
	Status               string `json:"status,omitempty"`
	WebsiteURL           string `json:"websiteUrl,omitempty"`
	// Repository           *model.Repository  `json:"repository"`
	Packages []AgentPackageInfo `json:"packages,omitempty"`
	Remotes  []model.Transport  `json:"remotes,omitempty"`
}

type AgentPackageInfo struct {
	RegistryType         string                `json:"registryType"`
	Identifier           string                `json:"identifier"`
	Version              string                `json:"version"`
	Transport            AgentTransport        `json:"transport"`
	RunTimeHint          string                `json:"runTimeHint,omitempty"`
	RuntimeArguments     []model.Argument      `json:"runtimeArguments,omitempty"`
	PackageArguments     []model.Argument      `json:"packageArguments,omitempty"`
	EnvironmentVariables []model.KeyValueInput `json:"environmentVariables,omitempty"`
}

type AgentTransport struct {
	URL     string                `json:"url,omitempty"`
	Headers []model.KeyValueInput `json:"headers,omitempty"`
}

// AgentRegistryExtensions mirrors official metadata stored separately
type AgentRegistryExtensions struct {
	Status      string    `json:"status"`
	PublishedAt time.Time `json:"publishedAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	IsLatest    bool      `json:"isLatest"`
}

type AgentResponseMeta struct {
	Official *AgentRegistryExtensions `json:"io.modelcontextprotocol.registry/official,omitempty"`
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
