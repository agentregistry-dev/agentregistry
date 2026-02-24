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
	Config          map[string]string `json:"config"`
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
