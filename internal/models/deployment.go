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
}

// KubernetesResource represents a deployment on Kubernetes (agent or MCP server)
type KubernetesResource struct {
	Type       string            `json:"type"`                // "agent", "mcpserver", "remotemcpserver"
	Name       string            `json:"name"`                // Resource name
	Namespace  string            `json:"namespace"`           // Kubernetes namespace
	Labels     map[string]string `json:"labels,omitempty"`    // Resource labels
	Status     string            `json:"status,omitempty"`    // Resource status (e.g., Ready, Pending)
	CreatedAt  *string           `json:"createdAt,omitempty"` // Creation timestamp
	IsExternal bool              `json:"isExternal"`          // true if not managed by registry
}
