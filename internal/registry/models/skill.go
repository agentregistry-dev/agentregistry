package models

import "time"

// SkillJSON mirrors the ServerJSON shape for now, defined locally
type SkillJSON struct {
    Name        string          `json:"name"`
    Title       string          `json:"title,omitempty"`
    Description string          `json:"description"`
    Version     string          `json:"version"`
    Status      string          `json:"status,omitempty"`
    WebsiteURL  string          `json:"websiteUrl,omitempty"`
    Repository  Repository      `json:"repository"`
    Packages    []PackageInfo   `json:"packages,omitempty"`
    Remotes     []RemoteInfo    `json:"remotes,omitempty"`
}

type Repository struct {
    URL    string `json:"url"`
    Source string `json:"source"`
}

type PackageInfo struct {
    RegistryType string `json:"registryType"`
    Identifier   string `json:"identifier"`
    Version      string `json:"version"`
    Transport    struct {
        Type string `json:"type"`
    } `json:"transport"`
}

type RemoteInfo struct {
    URL string `json:"url"`
}

// RegistryExtensions mirrors official metadata stored separately
type RegistryExtensions struct {
    Status      string    `json:"status"`
    PublishedAt time.Time `json:"publishedAt"`
    UpdatedAt   time.Time `json:"updatedAt"`
    IsLatest    bool      `json:"isLatest"`
}

type ResponseMeta struct {
    Official *RegistryExtensions `json:"io.modelcontextprotocol.registry/official,omitempty"`
}

type SkillResponse struct {
    Skill SkillJSON   `json:"skill"`
    Meta  ResponseMeta `json:"_meta"`
}

type Metadata struct {
    NextCursor string `json:"nextCursor,omitempty"`
    Count      int    `json:"count"`
}

type SkillListResponse struct {
    Skills   []SkillResponse `json:"skills"`
    Metadata Metadata        `json:"metadata"`
}


