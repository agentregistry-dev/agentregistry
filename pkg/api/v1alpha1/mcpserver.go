package v1alpha1

// MCPServer is the typed envelope for kind=MCPServer resources.
type MCPServer struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata ObjectMeta    `json:"metadata" yaml:"metadata"`
	Spec     MCPServerSpec `json:"spec" yaml:"spec"`
	Status   Status        `json:"status,omitzero" yaml:"status,omitempty"`
}

// MCPServerSpec is the MCP server's declarative body.
type MCPServerSpec struct {
	Title       string `json:"title,omitempty" yaml:"title,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// Source declares where the bundled MCP server comes from — Package (the
	// runnable distribution) and/or Repository (the source code).
	Source *MCPServerSource `json:"source,omitempty" yaml:"source,omitempty"`
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

// MCPTransport describes a transport endpoint — used both inside MCPPackage
// (to tag a package's preferred transport) and as a top-level remote target.
type MCPTransport struct {
	Type    string             `json:"type" yaml:"type"`
	URL     string             `json:"url,omitempty" yaml:"url,omitempty"`
	Headers []MCPKeyValueInput `json:"headers,omitempty" yaml:"headers,omitempty"`
}

// MCPPackage is a runnable distribution of the MCP server (stdio binary,
// container image, npm package, etc.).
type MCPPackage struct {
	RegistryType         string             `json:"registryType" yaml:"registryType"`
	RegistryBaseURL      string             `json:"registryBaseUrl,omitempty" yaml:"registryBaseUrl,omitempty"`
	Identifier           string             `json:"identifier" yaml:"identifier"`
	Version              string             `json:"version,omitempty" yaml:"version,omitempty"`
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
