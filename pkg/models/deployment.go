package models

import "time"

// Deployment represents a deployed server with its configuration
type Deployment struct {
	ServerName   string            `json:"serverName"`
	Version      string            `json:"version"`
	DeployedAt   time.Time         `json:"deployedAt"`
	UpdatedAt    time.Time         `json:"updatedAt"`
	Status       string            `json:"status"`
	Config       map[string]string `json:"config"`
	PreferRemote bool              `json:"preferRemote"`
	ResourceType string            `json:"resourceType"` // "mcp" or "agent"
	Runtime      string            `json:"runtime"`      // "local" or "kubernetes"
	IsExternal   bool              `json:"isExternal"`   // true if not managed by registry
}

// DeploymentFilter defines filtering options for deployment queries
type DeploymentFilter struct {
	Runtime      *string // "local" or "kubernetes"
	ResourceType *string // "mcp" or "agent"
}
