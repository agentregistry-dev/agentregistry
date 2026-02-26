package models

import "time"

// Deployment represents a deployed resource with unified deployment metadata.
type Deployment struct {
	ID              string            `json:"id"`
	ServerName      string            `json:"serverName"` // resource name (legacy field name retained for compatibility)
	Version         string            `json:"version"`
	ProviderID      string            `json:"providerId,omitempty"`
	ResourceType    string            `json:"resourceType"`
	Status          string            `json:"status"` // deploying, deployed, failed, cancelled, discovered
	Origin          string            `json:"origin"` // managed, discovered
	Region          string            `json:"region,omitempty"`
	CloudResourceID string            `json:"cloudResourceId,omitempty"`
	CloudMetadata   map[string]any    `json:"cloudMetadata,omitempty"`
	Env             map[string]string `json:"env"`
	ProviderConfig  map[string]any    `json:"providerConfig,omitempty"`
	PreferRemote    bool              `json:"preferRemote"`
	DeployedBy      string            `json:"deployedBy,omitempty"`
	Error           string            `json:"error,omitempty"`
	DeployedAt      time.Time         `json:"deployedAt"`
	UpdatedAt       time.Time         `json:"updatedAt"`

	IsExternal bool `json:"isExternal"`
}

// DeploymentFilter defines filtering options for deployment queries
type DeploymentFilter struct {
	Platform      *string // local, kubernetes
	ProviderID    *string
	ResourceType  *string // mcp or agent
	Status        *string
	Origin        *string
	ResourceName  *string // case-insensitive substring filter
	CloudResource *string
}

// DeploymentSummary is a compact deployment view embedded in catalog metadata.
type DeploymentSummary struct {
	ID         string    `json:"id"`
	ProviderID string    `json:"providerId,omitempty"`
	Status     string    `json:"status"`
	Origin     string    `json:"origin"`
	Version    string    `json:"version,omitempty"`
	DeployedAt time.Time `json:"deployedAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// ResourceDeploymentsMeta is the `_meta["aregistry.ai/deployments"]` payload.
type ResourceDeploymentsMeta struct {
	Deployments []DeploymentSummary `json:"deployments"`
	Count       int                 `json:"count"`
}
