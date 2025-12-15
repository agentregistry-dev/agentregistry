package types

import (
	"encoding/json"

	"github.com/modelcontextprotocol/registry/pkg/model"
)

// RegistryResponse represents the response from the MCP registry HTTP API
type RegistryResponse struct {
	Servers  []ServerEntry    `json:"servers"`
	Metadata RegistryMetadata `json:"metadata"`
}

// RegistryMetadata contains pagination information
type RegistryMetadata struct {
	Count      int    `json:"count"`
	NextCursor string `json:"nextCursor"`
}

// ServerEntry represents a server entry from the registry HTTP API
type ServerEntry struct {
	Server ServerSpec      `json:"server"`
	Meta   json.RawMessage `json:"_meta"`
}

// ServerSpec represents the server specification from the registry HTTP API
type ServerSpec struct {
	Name        string            `json:"name"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Version     string            `json:"version"`
	Status      string            `json:"status"`
	WebsiteURL  string            `json:"websiteUrl"`
	Repository  Repository        `json:"repository"`
	Packages    []model.Package   `json:"packages"`
	Remotes     []model.Transport `json:"remotes"`
}

// Repository represents the repository information from the registry HTTP API
type Repository struct {
	URL    string `json:"url"`
	Source string `json:"source"`
}
