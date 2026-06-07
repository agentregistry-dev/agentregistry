package v1alpha1

// MCPServer is the typed envelope for kind=MCPServer resources.
type MCPServer struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata ObjectMeta    `json:"metadata" yaml:"metadata"`
	Spec     MCPServerSpec `json:"spec" yaml:"spec"`
	Status   Status        `json:"status,omitzero" yaml:"status,omitempty"`
}

func init() {
	MustRegisterKind[*MCPServer, MCPServerSpec](KindMCPServer)
}

// MCPServerSpec is the MCP server's declarative body.
type MCPServerSpec struct {
	Title       string `json:"title,omitempty" yaml:"title,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// Source declares where the bundled MCP server comes from — Package (the
	// runnable distribution) and/or Repository (the source code).
	Source *MCPServerSource `json:"source,omitempty" yaml:"source,omitempty"`

	// Remote declares a remote MCP server instead of a bundled one. These are pre-existing
	// MCP servers that the registry does not deploy but can be referenced by Agents.
	Remote *MCPRemote `json:"remote,omitempty" yaml:"remote,omitempty"`

	// OpenAPI declares a REST API described by an OpenAPI 3.0 schema. The
	// registry does not deploy it; its endpoints are surfaced as MCP tools by
	// the platform serving the registry. The URL is the REST base, not an MCP
	// endpoint.
	OpenAPI *MCPServerOpenAPI `json:"openapi,omitempty" yaml:"openapi,omitempty"`
}

// MCPServerOpenAPI describes a REST API surfaced as an MCP server from its
// OpenAPI 3.0 schema. The endpoints declared in the schema are exposed as MCP
// tools by the platform serving the registry; the registry itself does not
// deploy the API.
type MCPServerOpenAPI struct {
	// URL is the REST API base (scheme://host[:port]). The scheme selects
	// whether requests use TLS. Must not carry a path, query, or fragment -
	// endpoint paths come from the schema.
	URL string `json:"url" yaml:"url"`

	// Schema is the inline OpenAPI 3.0 JSON schema.
	Schema string `json:"schema" yaml:"schema"`
}

// MCPRemote describes a pre-running remote MCP server that the registry
// does not deploy. Distinct from MCPTransport (used inside MCPPackage to
// describe a deployable package's transport) because remote headers carry
// only static name/value pairs - no templating.
type MCPRemote struct {
	Type    string       `json:"type" yaml:"type"`
	URL     string       `json:"url" yaml:"url"`
	Headers []HTTPHeader `json:"headers,omitempty" yaml:"headers,omitempty"`
}

// HTTPHeader is an HTTP header sent on requests to a remote MCP server.
type HTTPHeader struct {
	Name  string `json:"name" yaml:"name"`
	Value string `json:"value,omitempty" yaml:"value,omitempty"`
}

// MCPServerSource is the distribution origin of a bundled MCP server —
// either a published artifact (Package) or a source repository the
// registry builds from.
type MCPServerSource struct {
	// Package is the runnable distribution (stdio binary, container image,
	// npm package, etc.) of this MCP server.
	Package *MCPPackage `json:"package,omitempty" yaml:"package,omitempty"`

	// Repository links to the source code the package was built from.
	Repository *Repository `json:"repository,omitempty" yaml:"repository,omitempty"`
}

// MCPTransport describes how a deployable MCPPackage exposes itself. Used
// only inside MCPPackage; remotes use MCPRemote, which carries its own URL.
// For http, the listen Port and endpoint Path are set explicitly because the
// host is constructed at deploy time. Both are ignored for stdio.
type MCPTransport struct {
	Type string `json:"type" yaml:"type"`                     // "http" | "stdio"
	Port uint16 `json:"port,omitempty" yaml:"port,omitempty"` // http listen port 1-65535 (ignored for stdio)
	Path string `json:"path,omitempty" yaml:"path,omitempty"` // http endpoint path, e.g. "/mcp" (ignored for stdio)
}

// MCPPackage is a runnable distribution of the MCP server (stdio binary,
// container image, npm package, etc.).
type MCPPackage struct {
	RegistryType    string `json:"registryType" yaml:"registryType"`
	RegistryBaseURL string `json:"registryBaseUrl,omitempty" yaml:"registryBaseUrl,omitempty"`
	Identifier      string `json:"identifier" yaml:"identifier"`
	Version         string `json:"version,omitempty" yaml:"version,omitempty"`

	// ServerName is the MCP-ecosystem catalogue identity claimed by this
	// package. The registry's ownership validator compares this value
	// against the identity the package's publisher embedded in the upstream
	// artifact (NPM: `mcpName`, PyPI: `mcp-name`,
	// OCI: `io.modelcontextprotocol.server.name` label).
	//
	// Required for every registryType except `mcpb` (which has no ownership
	// concept). Accepts any identifier-shaped string Both single-segment (`my-mcp`)
	// and namespace/name (`io.example/foo`) forms validate, so the value can match
	// whatever the publisher embedded regardless of shape.
	ServerName string `json:"serverName,omitempty" yaml:"serverName,omitempty"`

	FileSHA256           string             `json:"fileSha256,omitempty" yaml:"fileSha256,omitempty"`
	RuntimeHint          string             `json:"runtimeHint,omitempty" yaml:"runtimeHint,omitempty"`
	Transport            MCPTransport       `json:"transport" yaml:"transport"`
	RuntimeArguments     []MCPArgument      `json:"runtimeArguments,omitempty" yaml:"runtimeArguments,omitempty"`
	PackageArguments     []MCPArgument      `json:"packageArguments,omitempty" yaml:"packageArguments,omitempty"`
	EnvironmentVariables []MCPKeyValueInput `json:"environmentVariables,omitempty" yaml:"environmentVariables,omitempty"`
}

// MCPInputVariable describes a parameterizable value referenced from
// MCPArgument.Variables or MCPKeyValueInput.Variables.
type MCPInputVariable struct {
	Description string   `json:"description,omitempty" yaml:"description,omitempty"`
	IsRequired  bool     `json:"isRequired,omitempty" yaml:"isRequired,omitempty"`
	Format      string   `json:"format,omitempty" yaml:"format,omitempty"`
	Value       string   `json:"value,omitempty" yaml:"value,omitempty"`
	IsSecret    bool     `json:"isSecret,omitempty" yaml:"isSecret,omitempty"`
	Default     string   `json:"default,omitempty" yaml:"default,omitempty"`
	Placeholder string   `json:"placeholder,omitempty" yaml:"placeholder,omitempty"`
	Choices     []string `json:"choices,omitempty" yaml:"choices,omitempty"`
}

// MCPArgument.Type values. Kept as string literals to match the YAML wire
// format; platform translators compare against these.
const (
	MCPArgumentTypePositional = "positional"
	MCPArgumentTypeNamed      = "named"
)

// MCPArgument is a positional or named argument passed to a package's runtime.
type MCPArgument struct {
	Type        string                      `json:"type" yaml:"type"`
	Name        string                      `json:"name,omitempty" yaml:"name,omitempty"`
	ValueHint   string                      `json:"valueHint,omitempty" yaml:"valueHint,omitempty"`
	IsRepeated  bool                        `json:"isRepeated,omitempty" yaml:"isRepeated,omitempty"`
	Description string                      `json:"description,omitempty" yaml:"description,omitempty"`
	IsRequired  bool                        `json:"isRequired,omitempty" yaml:"isRequired,omitempty"`
	Format      string                      `json:"format,omitempty" yaml:"format,omitempty"`
	Value       string                      `json:"value,omitempty" yaml:"value,omitempty"`
	IsSecret    bool                        `json:"isSecret,omitempty" yaml:"isSecret,omitempty"`
	Default     string                      `json:"default,omitempty" yaml:"default,omitempty"`
	Placeholder string                      `json:"placeholder,omitempty" yaml:"placeholder,omitempty"`
	Choices     []string                    `json:"choices,omitempty" yaml:"choices,omitempty"`
	Variables   map[string]MCPInputVariable `json:"variables,omitempty" yaml:"variables,omitempty"`
}

// MCPKeyValueInput represents an environment variable or HTTP header input.
type MCPKeyValueInput struct {
	Name        string                      `json:"name" yaml:"name"`
	Description string                      `json:"description,omitempty" yaml:"description,omitempty"`
	IsRequired  bool                        `json:"isRequired,omitempty" yaml:"isRequired,omitempty"`
	Format      string                      `json:"format,omitempty" yaml:"format,omitempty"`
	Value       string                      `json:"value,omitempty" yaml:"value,omitempty"`
	IsSecret    bool                        `json:"isSecret,omitempty" yaml:"isSecret,omitempty"`
	Default     string                      `json:"default,omitempty" yaml:"default,omitempty"`
	Placeholder string                      `json:"placeholder,omitempty" yaml:"placeholder,omitempty"`
	Choices     []string                    `json:"choices,omitempty" yaml:"choices,omitempty"`
	Variables   map[string]MCPInputVariable `json:"variables,omitempty" yaml:"variables,omitempty"`
}
