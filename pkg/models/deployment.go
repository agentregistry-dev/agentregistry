package models

import (
	"encoding/json"
	"time"
)

// Deployment represents a deployed resource with unified deployment metadata.
type Deployment struct {
	ID               string            `json:"id"`
	ServerName       string            `json:"serverName"` // deployed resource name
	Version          string            `json:"version"`
	ProviderID       string            `json:"providerId,omitempty"`
	ResourceType     string            `json:"resourceType"`
	Status           string            `json:"status"` // deploying, deployed, failed, cancelled, discovered
	Origin           string            `json:"origin"` // managed, discovered
	Env              map[string]string `json:"env"`
	ProviderConfig   JSONObject        `json:"providerConfig,omitempty"`
	ProviderMetadata JSONObject        `json:"providerMetadata,omitempty"`
	PreferRemote     bool              `json:"preferRemote"`
	Error            string            `json:"error,omitempty"`
	DeployedAt       time.Time         `json:"deployedAt"`
	UpdatedAt        time.Time         `json:"updatedAt"`
}

// DeploymentActionResult captures provider-specific execution outcome from adapters.
// The registry service owns persistence and applies this result to deployment rows.
type DeploymentActionResult struct {
	// Status should be a terminal or in-flight deployment status (for example: deployed, deploying, failed).
	// When empty, the service falls back to a status based on whether Deploy returned an error.
	Status string `json:"status,omitempty"`
	// Error contains provider-specific failure details, if any.
	Error string `json:"error,omitempty"`
	// ProviderConfig stores provider-specific effective config to persist.
	ProviderConfig JSONObject `json:"providerConfig,omitempty"`
	// ProviderMetadata stores provider-specific runtime metadata to persist.
	ProviderMetadata JSONObject `json:"providerMetadata,omitempty"`
}

// DeploymentStatePatch describes partial deployment state updates.
// Nil fields are left unchanged.
type DeploymentStatePatch struct {
	Status           *string
	Error            *string
	ProviderConfig   *JSONObject
	ProviderMetadata *JSONObject
}

type KubernetesProviderMetadata struct {
	IsExternal bool   `json:"isExternal"`
	Namespace  string `json:"namespace,omitempty"`
}

type JSONObject map[string]any

func (o JSONObject) UnmarshalInto(v any) error {
	if o == nil {
		return nil
	}
	b, err := json.Marshal(o)
	if err != nil {
		return err
	}

	return json.Unmarshal(b, v)
}

func UnmarshalFrom(v any) (JSONObject, error) {
	if v == nil {
		return JSONObject{}, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}

	o := JSONObject{}
	return o, json.Unmarshal(b, &o)
}

// DeploymentFilter defines filtering options for deployment queries
type DeploymentFilter struct {
	Platform     *string // local, kubernetes
	ProviderID   *string
	ResourceType *string // mcp or agent
	Status       *string
	Origin       *string
	ResourceName *string // case-insensitive substring filter
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
