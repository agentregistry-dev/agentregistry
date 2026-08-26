package v1alpha1

// MCPServer is the typed envelope for kind=MCPServer resources.
type MCPServer struct {
	TypeMeta `json:",inline" yaml:",inline"`
	// metadata is part of MCPServer.
	// +required
	Metadata ObjectMeta `json:"metadata" yaml:"metadata"`
	// spec is part of MCPServer.
	// +required
	Spec MCPServerSpec `json:"spec" yaml:"spec"`
	// status is part of MCPServer.
	// +optional
	Status Status `json:"status,omitzero" yaml:"status,omitempty"`
}

func init() {
	MustRegisterKind[*MCPServer, MCPServerSpec](KindMCPServer)
}

// MCPServerSpec is the MCP server's declarative body.
type MCPServerSpec struct {
	// title is part of MCPServerSpec.
	// +optional
	Title string `json:"title,omitempty" yaml:"title,omitempty"`
	// description is part of MCPServerSpec.
	// +optional
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// iconUrl is the image a catalog UI shows for this MCP server. Either an
	// absolute https:// URL or a root-relative path served by the UI.
	// +optional
	IconURL string `json:"iconUrl,omitempty" yaml:"iconUrl,omitempty"`

	// source declares where the bundled MCP server comes from — Package (the
	// runnable distribution) and/or Repository (the source code).
	// +optional
	Source *MCPServerSource `json:"source,omitempty" yaml:"source,omitempty"`

	// remote declares a remote MCP server instead of a bundled one. These are pre-existing
	// MCP servers that the registry does not deploy but can be referenced by Agents.
	// +optional
	Remote *MCPRemote `json:"remote,omitempty" yaml:"remote,omitempty"`
}

// MCPRemote describes a pre-running remote MCP server that the registry
// does not deploy. Distinct from MCPTransport (used inside MCPPackage to
// describe a deployable package's transport) because remote headers carry
// only static name/value pairs - no templating.
type MCPRemote struct {
	// type is part of MCPRemote.
	// +required
	// +kubebuilder:validation:MinLength=1
	Type string `json:"type" yaml:"type"`
	// url is part of MCPRemote.
	// +required
	// +kubebuilder:validation:MinLength=1
	URL string `json:"url" yaml:"url"`
	// headers is part of MCPRemote.
	// +optional
	Headers []HTTPHeader `json:"headers,omitempty" yaml:"headers,omitempty"`
}

// HTTPHeader is an HTTP header sent on requests to a remote MCP server.
type HTTPHeader struct {
	// name is part of HTTPHeader.
	// +required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name" yaml:"name"`
	// value is part of HTTPHeader.
	// +optional
	Value string `json:"value,omitempty" yaml:"value,omitempty"`
}

// MCPServerSource is the distribution origin of a bundled MCP server —
// either a published artifact (Package) or a source repository the
// registry builds from.
type MCPServerSource struct {
	// package is the runnable distribution (stdio binary, container image,
	// npm package, etc.) of this MCP server.
	// +optional
	Package *MCPPackage `json:"package,omitempty" yaml:"package,omitempty"`

	// repository links to the source code the package was built from.
	// +optional
	Repository *Repository `json:"repository,omitempty" yaml:"repository,omitempty"`
}

// MCPTransport describes how a deployable MCPPackage exposes itself. Used
// only inside MCPPackage; remotes use MCPRemote, which carries its own URL.
// For http, the listen Port and endpoint Path are set explicitly because the
// host is constructed at deploy time. Both are ignored for stdio.
type MCPTransport struct {
	// type is part of MCPTransport.
	// +required
	// +kubebuilder:validation:MinLength=1
	Type string `json:"type" yaml:"type"` // "http" | "stdio"
	// port is part of MCPTransport.
	// +optional
	Port uint16 `json:"port,omitempty" yaml:"port,omitempty"` // http listen port 1-65535 (ignored for stdio)
	// path is part of MCPTransport.
	// +optional
	Path string `json:"path,omitempty" yaml:"path,omitempty"` // http endpoint path, e.g. "/mcp" (ignored for stdio)
}

// MCPPackage is a runnable distribution of an MCP server, grouped by
// concern: Origin (what to fetch), Launch (how to start it), Transport
// (how to talk to it).
type MCPPackage struct {
	// origin is part of MCPPackage.
	// +required
	Origin MCPPackageOrigin `json:"origin" yaml:"origin"`
	// launch is part of MCPPackage.
	// +optional
	Launch *MCPPackageLaunch `json:"launch,omitempty" yaml:"launch,omitempty"`
	// transport is part of MCPPackage.
	// +required
	Transport MCPTransport `json:"transport" yaml:"transport"`
}

// MCPPackageOrigin identifies the package and where to fetch it. The Type
// discriminator selects which per-type sub-struct must be set; exactly one
// of NPM/PyPI/OCI is non-nil, matching Type.
type MCPPackageOrigin struct {
	// type is part of MCPPackageOrigin.
	// +required
	Type MCPPackageOriginType `json:"type" yaml:"type"`
	// identifier is part of MCPPackageOrigin.
	// +required
	// +kubebuilder:validation:MinLength=1
	Identifier string `json:"identifier" yaml:"identifier"`

	// npm is part of MCPPackageOrigin.
	// +optional
	NPM *MCPPackageOriginNPM `json:"npm,omitempty"  yaml:"npm,omitempty"`
	// pypi is part of MCPPackageOrigin.
	// +optional
	PyPI *MCPPackageOriginPyPI `json:"pypi,omitempty" yaml:"pypi,omitempty"`
	// oci is part of MCPPackageOrigin.
	// +optional
	OCI *MCPPackageOriginOCI `json:"oci,omitempty"  yaml:"oci,omitempty"`
}

type MCPPackageOriginType string

const (
	MCPPackageOriginTypeNPM  MCPPackageOriginType = "npm"
	MCPPackageOriginTypePyPI MCPPackageOriginType = "pypi"
	MCPPackageOriginTypeOCI  MCPPackageOriginType = "oci"
)

// MCPPackageOriginNPM holds npm-specific fetch inputs.
type MCPPackageOriginNPM struct {
	// version is part of MCPPackageOriginNPM.
	// +required
	// +kubebuilder:validation:MinLength=1
	Version string `json:"version" yaml:"version"`
	// mirror is part of MCPPackageOriginNPM.
	// +optional
	Mirror string `json:"mirror,omitempty" yaml:"mirror,omitempty"`
	// serverName is part of MCPPackageOriginNPM.
	// +required
	// +kubebuilder:validation:MinLength=1
	ServerName string `json:"serverName" yaml:"serverName"`
}

// MCPPackageOriginPyPI holds pypi-specific fetch inputs.
type MCPPackageOriginPyPI struct {
	// version is part of MCPPackageOriginPyPI.
	// +required
	// +kubebuilder:validation:MinLength=1
	Version string `json:"version" yaml:"version"`
	// mirror is part of MCPPackageOriginPyPI.
	// +optional
	Mirror string `json:"mirror,omitempty" yaml:"mirror,omitempty"`
	// serverName is part of MCPPackageOriginPyPI.
	// +required
	// +kubebuilder:validation:MinLength=1
	ServerName string `json:"serverName" yaml:"serverName"`
}

// MCPPackageOriginOCI holds oci-specific fetch inputs. Version is encoded
// in Identifier (e.g. "ghcr.io/foo/bar:1.0.0" or "...@sha256:..."); bare
// image refs that would silently resolve `:latest` are rejected by the
// validator.
type MCPPackageOriginOCI struct {
	// serverName is part of MCPPackageOriginOCI.
	// +required
	// +kubebuilder:validation:MinLength=1
	ServerName string `json:"serverName" yaml:"serverName"`
}

// MCPPackageLaunch declares how to start the fetched package. If Launch
// is nil, the resolver derives Command and Args from Origin.Type defaults
// (npm → "npx -y <id>@<ver>"; pypi → "uvx <id>==<ver>"; oci → image
// entrypoint). If Launch is set, the manifest owns Command and Args
// verbatim — no implicit identifier injection. Command may be empty only
// for oci.
type MCPPackageLaunch struct {
	// command is part of MCPPackageLaunch.
	// +optional
	Command string `json:"command,omitempty" yaml:"command,omitempty"`
	// args is part of MCPPackageLaunch.
	// +optional
	Args []MCPArgument `json:"args,omitempty" yaml:"args,omitempty"`
	// env is part of MCPPackageLaunch.
	// +optional
	Env []MCPKeyValueInput `json:"env,omitempty" yaml:"env,omitempty"`
}

// MCPArgument is one command-line argument.
type MCPArgument struct {
	// type is part of MCPArgument.
	// +required
	Type MCPArgumentType `json:"type" yaml:"type"`
	// name is part of MCPArgument.
	// +optional
	Name string `json:"name,omitempty" yaml:"name,omitempty"`
	// value is part of MCPArgument.
	// +optional
	Value string `json:"value,omitempty" yaml:"value,omitempty"`
}

type MCPArgumentType string

const (
	MCPArgumentTypePositional MCPArgumentType = "positional"
	MCPArgumentTypeNamed      MCPArgumentType = "named"
)

// MCPKeyValueInput is one declared environment variable.
type MCPKeyValueInput struct {
	// name is part of MCPKeyValueInput.
	// +required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name" yaml:"name"`
	// value is part of MCPKeyValueInput.
	// +optional
	Value string `json:"value,omitempty" yaml:"value,omitempty"`
	// isRequired is part of MCPKeyValueInput.
	// +optional
	IsRequired bool `json:"isRequired,omitempty" yaml:"isRequired,omitempty"`
}
