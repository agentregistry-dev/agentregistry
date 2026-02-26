package models

import "time"

// PromptJSON is the stored JSONB payload for a prompt registry entry.
// A prompt is a named, versioned text string (e.g. a system prompt for an agent).
type PromptJSON struct {
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Version     string `json:"version" yaml:"version"`
	Content     string `json:"content" yaml:"content"`
}

// PromptRegistryExtensions mirrors official metadata stored separately.
type PromptRegistryExtensions struct {
	Status      string    `json:"status"`
	PublishedAt time.Time `json:"publishedAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	IsLatest    bool      `json:"isLatest"`
}

// PromptResponseMeta contains metadata about a prompt response.
type PromptResponseMeta struct {
	Official *PromptRegistryExtensions `json:"io.modelcontextprotocol.registry/official,omitempty"`
}

// PromptResponse wraps a PromptJSON with its registry metadata.
type PromptResponse struct {
	Prompt PromptJSON         `json:"prompt"`
	Meta   PromptResponseMeta `json:"_meta"`
}

// PromptMetadata contains pagination info for prompt list responses.
type PromptMetadata struct {
	NextCursor string `json:"nextCursor,omitempty"`
	Count      int    `json:"count"`
}

// PromptListResponse is the paginated list response for prompts.
type PromptListResponse struct {
	Prompts  []PromptResponse `json:"prompts"`
	Metadata PromptMetadata   `json:"metadata"`
}
