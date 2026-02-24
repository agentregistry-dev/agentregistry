package models

import "time"

// Provider represents a concrete deployment target instance.
// Examples: a specific kube cluster.
type Provider struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Platform  string         `json:"platform"` // local, kubernetes
	Config    map[string]any `json:"config,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

// CreateProviderInput defines inputs for provider creation.
type CreateProviderInput struct {
	ID       string         `json:"id,omitempty"`
	Name     string         `json:"name"`
	Platform string         `json:"platform"`
	Config   map[string]any `json:"config,omitempty"`
}

// UpdateProviderInput defines inputs for provider updates.
type UpdateProviderInput struct {
	Name   *string        `json:"name,omitempty"`
	Config map[string]any `json:"config,omitempty"`
}
