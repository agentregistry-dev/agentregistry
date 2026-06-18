// Package mcpregistry provides a read-only compatibility representation of
// AgentRegistry's MCPServer resources in the shape defined by the official
// MCP Registry (github.com/modelcontextprotocol/registry) `server.json`
// v0.1 specification.
//
// AgentRegistry began as a fork of the official MCP Registry but has since
// reorganized its public API around Kubernetes-style v1alpha1 resources
// (apiVersion/kind/metadata/spec, MCPServer at `/v0/mcpservers`). That broke
// the contract registry-aware clients (VS Code's MCP gallery, etc.) expect.
// This package re-projects MCPServer rows into the upstream `server.json`
// shape so those clients can discover servers again, without rolling back the
// native API.
//
// The types here mirror the v0.1 frozen spec exactly — field names use the
// camelCase casing emitted by registry.modelcontextprotocol.io, list items are
// wrapped in {server, _meta}, and the registry-managed metadata lives under the
// reverse-DNS `_meta` key. The translation is one-directional (v1alpha1 →
// server.json) and read-only; there is no publish/write path here.
package mcpregistry

// SchemaURL is the `$schema` value emitted on every ServerDetail. It pins the
// frozen v0.1 schema revision this package targets.
const SchemaURL = "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json"

// OfficialMetaKey is the reverse-DNS `_meta` extension key under which the
// registry exposes its server-managed metadata (status, timestamps, isLatest).
const OfficialMetaKey = "io.modelcontextprotocol.registry/official"

// ServerListResponse is the envelope returned by `GET /v0.1/servers` and the
// versions-list endpoint: a page of servers plus pagination metadata.
type ServerListResponse struct {
	Servers  []ServerResponse `json:"servers"`
	Metadata ListMetadata     `json:"metadata"`
}

// ListMetadata carries cursor-based pagination state. NextCursor is empty on
// the final page.
type ListMetadata struct {
	NextCursor string `json:"nextCursor,omitempty"`
	Count      int    `json:"count"`
}

// ServerResponse is one entry in a list (or the body of a single-server GET):
// the server detail plus the registry-managed `_meta` block.
//
// Nested document types (ServerRepository, ServerPackage, ...) carry a
// disambiguating prefix rather than the bare spec names (Repository, Package,
// ...) because Huma derives OpenAPI component names from the bare Go type
// name; the bare names would collide with the v1alpha1 schemas registered on
// the same API.
type ServerResponse struct {
	Server ServerDetail  `json:"server"`
	Meta   *ResponseMeta `json:"_meta,omitempty"`
}

// ResponseMeta is the `_meta` extension block. Only the official
// registry-managed sub-object is populated by this read-only shim.
type ResponseMeta struct {
	Official *OfficialMeta `json:"io.modelcontextprotocol.registry/official,omitempty"`
}

// OfficialMeta is the registry-managed metadata clients read to learn a
// server's lifecycle status and freshness. Timestamps are RFC3339.
type OfficialMeta struct {
	Status          string `json:"status,omitempty"`
	StatusChangedAt string `json:"statusChangedAt,omitempty"`
	PublishedAt     string `json:"publishedAt,omitempty"`
	UpdatedAt       string `json:"updatedAt,omitempty"`
	IsLatest        bool   `json:"isLatest"`
}

// ServerDetail is the core `server.json` document.
type ServerDetail struct {
	Schema      string            `json:"$schema,omitempty"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Title       string            `json:"title,omitempty"`
	Version     string            `json:"version"`
	Repository  *ServerRepository `json:"repository,omitempty"`
	WebsiteURL  string            `json:"websiteUrl,omitempty"`
	Packages    []ServerPackage   `json:"packages,omitempty"`
	Remotes     []ServerTransport `json:"remotes,omitempty"`
}

// ServerRepository links a server to its source code.
type ServerRepository struct {
	URL       string `json:"url"`
	Source    string `json:"source,omitempty"`
	ID        string `json:"id,omitempty"`
	Subfolder string `json:"subfolder,omitempty"`
}

// ServerPackage describes one runnable distribution of the server
// (npm/pypi/oci/...).
type ServerPackage struct {
	RegistryType         string           `json:"registryType"`
	RegistryBaseURL      string           `json:"registryBaseUrl,omitempty"`
	Identifier           string           `json:"identifier"`
	Version              string           `json:"version"`
	FileSHA256           string           `json:"fileSha256,omitempty"`
	RuntimeHint          string           `json:"runtimeHint,omitempty"`
	Transport            ServerTransport  `json:"transport"`
	RuntimeArguments     []ServerArgument `json:"runtimeArguments,omitempty"`
	PackageArguments     []ServerArgument `json:"packageArguments,omitempty"`
	EnvironmentVariables []ServerInput    `json:"environmentVariables,omitempty"`
}

// ServerTransport describes how to talk to a package or remote. For packages
// the type is one of "stdio" | "streamable-http" | "sse"; remotes use
// "streamable-http" | "sse" and carry a URL.
type ServerTransport struct {
	Type    string        `json:"type"`
	URL     string        `json:"url,omitempty"`
	Headers []ServerInput `json:"headers,omitempty"`
}

// ServerArgument is one command-line argument (positional or named).
type ServerArgument struct {
	Type  string `json:"type"`
	Name  string `json:"name,omitempty"`
	Value string `json:"value,omitempty"`
}

// ServerInput is a name/value pair used for environment variables and remote
// headers.
type ServerInput struct {
	Name       string `json:"name"`
	Value      string `json:"value,omitempty"`
	IsRequired bool   `json:"isRequired,omitempty"`
}
