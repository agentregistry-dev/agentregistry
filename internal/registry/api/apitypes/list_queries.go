package apitypes

// ListAgentsInput represents shared query parameters for listing agents.
type ListAgentsInput struct {
	Cursor                 string  `query:"cursor" json:"cursor,omitempty" doc:"Pagination cursor" required:"false" example:"agent-cursor-123"`
	Limit                  int     `query:"limit" json:"limit,omitempty" doc:"Number of items per page" default:"30" minimum:"1" maximum:"100" example:"50"`
	UpdatedSince           string  `query:"updated_since" json:"updated_since,omitempty" doc:"Filter agents updated since timestamp (RFC3339 datetime)" required:"false" example:"2025-08-07T13:15:04.280Z"`
	Search                 string  `query:"search" json:"search,omitempty" doc:"Search agents by name (substring match)" required:"false" example:"filesystem"`
	Version                string  `query:"version" json:"version,omitempty" doc:"Filter by version ('latest' for latest version, or an exact version like '1.2.3')" required:"false" example:"latest"`
	Semantic               bool    `query:"semantic_search" json:"semantic_search,omitempty" doc:"Use semantic search for the search term"`
	SemanticMatchThreshold float64 `query:"semantic_threshold" json:"semantic_threshold,omitempty" doc:"Optional maximum cosine distance when semantic_search is enabled" required:"false"`
}

// ListServersInput represents shared query parameters for listing servers.
type ListServersInput struct {
	Cursor                 string  `query:"cursor" json:"cursor,omitempty" doc:"Pagination cursor" required:"false" example:"server-cursor-123"`
	Limit                  int     `query:"limit" json:"limit,omitempty" doc:"Number of items per page" default:"30" minimum:"1" maximum:"100" example:"50"`
	UpdatedSince           string  `query:"updated_since" json:"updated_since,omitempty" doc:"Filter servers updated since timestamp (RFC3339 datetime)" required:"false" example:"2025-08-07T13:15:04.280Z"`
	Search                 string  `query:"search" json:"search,omitempty" doc:"Search servers by name (substring match)" required:"false" example:"filesystem"`
	Version                string  `query:"version" json:"version,omitempty" doc:"Filter by version ('latest' for latest version, or an exact version like '1.2.3')" required:"false" example:"latest"`
	Semantic               bool    `query:"semantic_search" json:"semantic_search,omitempty" doc:"Use semantic search for the search term (hybrid with substring filter when search is set)" default:"false"`
	SemanticMatchThreshold float64 `query:"semantic_threshold" json:"semantic_threshold,omitempty" doc:"Optional maximum distance for semantic matches (cosine distance)" required:"false"`
}

// ListSkillsInput represents shared query parameters for listing skills.
type ListSkillsInput struct {
	Cursor       string `query:"cursor" json:"cursor,omitempty" doc:"Pagination cursor" required:"false" example:"skill-cursor-123"`
	Limit        int    `query:"limit" json:"limit,omitempty" doc:"Number of items per page" default:"30" minimum:"1" maximum:"100" example:"50"`
	UpdatedSince string `query:"updated_since" json:"updated_since,omitempty" doc:"Filter skills updated since timestamp (RFC3339 datetime)" required:"false" example:"2025-08-07T13:15:04.280Z"`
	Search       string `query:"search" json:"search,omitempty" doc:"Search skills by name (substring match)" required:"false" example:"filesystem"`
	Version      string `query:"version" json:"version,omitempty" doc:"Filter by version ('latest' for latest version, or an exact version like '1.2.3')" required:"false" example:"latest"`
}

// DeploymentsListInput represents shared query parameters for listing deployments.
type DeploymentsListInput struct {
	Platform     string `query:"platform" json:"platform,omitempty" doc:"Filter by provider platform type (matches registered provider platforms)" example:"local"`
	ProviderID   string `query:"providerId" json:"providerId,omitempty" doc:"Filter by provider instance ID"`
	ResourceType string `query:"resourceType" json:"resourceType,omitempty" doc:"Filter by resource type (mcp, agent)" example:"mcp" enum:"mcp,agent"`
	Status       string `query:"status" json:"status,omitempty" doc:"Filter by deployment status"`
	Origin       string `query:"origin" json:"origin,omitempty" doc:"Filter by deployment origin (managed, discovered)" enum:"managed,discovered"`
	ResourceName string `query:"resourceName" json:"resourceName,omitempty" doc:"Case-insensitive substring filter on resource name"`
}
