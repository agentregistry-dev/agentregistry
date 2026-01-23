package models

import (
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// ServerSemanticMeta carries semantic search metadata for servers.
type ServerSemanticMeta struct {
	Score float64 `json:"score"`
}

// ServerResponseMeta mirrors the MCP ResponseMeta but adds semantic metadata.
type ServerResponseMeta struct {
	Official *apiv0.RegistryExtensions `json:"io.modelcontextprotocol.registry/official,omitempty"`
	Semantic *ServerSemanticMeta       `json:"aregistry.ai/semantic,omitempty"`
}

// ServerResponse is the server API shape with registry-managed metadata.
type ServerResponse struct {
	Server apiv0.ServerJSON   `json:"server"`
	Meta   ServerResponseMeta `json:"_meta"`
}

// ServerMetadata holds pagination info for server listings.
type ServerMetadata struct {
	NextCursor string `json:"nextCursor,omitempty"`
	Count      int    `json:"count"`
}

// ServerListResponse wraps a list response.
type ServerListResponse struct {
	Servers  []ServerResponse `json:"servers"`
	Metadata ServerMetadata   `json:"metadata"`
}
