package models

import "time"

// SkillJSON mirrors the ServerJSON shape for now, defined locally
type SkillJSON struct {
	Name        string             `json:"name"`
	Title       string             `json:"title,omitempty"`
	Category    string             `json:"category,omitempty"`
	Description string             `json:"description"`
	Version     string             `json:"version"`
	Status      string             `json:"status,omitempty"`
	WebsiteURL  string             `json:"websiteUrl,omitempty"`
	Repository  *SkillRepository   `json:"repository,omitempty"`
	Packages    []SkillPackageInfo `json:"packages,omitempty"`
	Remotes     []SkillRemoteInfo  `json:"remotes,omitempty"`
}

type SkillRepository struct {
	URL    string `json:"url"`
	Source string `json:"source"`
}

type SkillPackageInfo struct {
	RegistryType string `json:"registryType"`
	Identifier   string `json:"identifier"`
	Version      string `json:"version"`
	Transport    struct {
		Type string `json:"type"`
	} `json:"transport"`
}

type SkillRemoteInfo struct {
	URL string `json:"url"`
}

// RegistryExtensions mirrors official metadata stored separately
type SkillRegistryExtensions struct {
	Status      string    `json:"status"`
	PublishedAt time.Time `json:"publishedAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	IsLatest    bool      `json:"isLatest"`
}

type SkillResponseMeta struct {
	Official *SkillRegistryExtensions `json:"io.modelcontextprotocol.registry/official,omitempty"`
}

type SkillResponse struct {
	Skill SkillJSON         `json:"skill"`
	Meta  SkillResponseMeta `json:"_meta"`
}

type SkillMetadata struct {
	NextCursor string `json:"nextCursor,omitempty"`
	Count      int    `json:"count"`
}

type SkillListResponse struct {
	Skills   []SkillResponse `json:"skills"`
	Metadata SkillMetadata   `json:"metadata"`
}
