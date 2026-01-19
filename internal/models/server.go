package models

import (
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// ServerResponseMeta extends the external ResponseMeta with ApprovalStatus
type ServerResponseMeta struct {
	Official       *apiv0.RegistryExtensions `json:"io.modelcontextprotocol.registry/official,omitempty"`
	ApprovalStatus ApprovalStatus            `json:"approvalStatus,omitempty"`
}

// ServerResponse wraps the external ServerResponse with our custom Meta
type ServerResponse struct {
	Server apiv0.ServerJSON   `json:"server"`
	Meta   ServerResponseMeta `json:"_meta"`
}

// ServerListResponse wraps the external ServerListResponse
type ServerListResponse struct {
	Servers  []ServerResponse `json:"servers"`
	Metadata apiv0.Metadata   `json:"metadata"`
}
