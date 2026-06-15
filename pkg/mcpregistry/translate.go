package mcpregistry

import (
	"fmt"
	"strings"
	"time"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// nameSep separates the namespace from the resource name in a catalogue
// server name. The upstream spec requires a name in reverse-DNS form with
// exactly one forward slash; "<namespace>/<name>" satisfies that, is unique
// across the flattened all-namespaces catalogue, and round-trips through
// ParseServerName.
const nameSep = "/"

// defaultVersion is emitted when a server has no derivable version. The spec
// requires a non-empty version string; this is a deterministic placeholder.
const defaultVersion = "0.0.0"

// ServerName builds the catalogue name for an MCPServer from its AgentRegistry
// (namespace, name). An empty namespace is treated as the default namespace so
// the wire name is always fully qualified.
func ServerName(namespace, name string) string {
	if namespace == "" {
		namespace = v1alpha1.DefaultNamespace
	}
	return namespace + nameSep + name
}

// ParseServerName is the inverse of ServerName: it splits a catalogue server
// name back into its AgentRegistry (namespace, name). It returns an error for
// names that don't have exactly one slash with non-empty parts, which is what
// the get-by-name routes use to reject malformed identifiers.
func ParseServerName(serverName string) (namespace, name string, err error) {
	ns, n, ok := strings.Cut(serverName, nameSep)
	if !ok || ns == "" || n == "" || strings.Contains(n, nameSep) {
		return "", "", fmt.Errorf("invalid server name %q: expected <namespace>/<name>", serverName)
	}
	return ns, n, nil
}

// FromMCPServer translates a typed v1alpha1.MCPServer into the read-only
// v0.1 ServerResponse (server detail + registry-managed _meta).
func FromMCPServer(s *v1alpha1.MCPServer) ServerResponse {
	ns := s.Metadata.NamespaceOrDefault()
	detail := ServerDetail{
		Schema:      SchemaURL,
		Name:        ServerName(ns, s.Metadata.Name),
		Title:       s.Spec.Title,
		Description: descriptionOf(s),
		Version:     versionOf(s),
	}
	if src := s.Spec.Source; src != nil {
		if src.Repository != nil {
			detail.Repository = repositoryOf(src.Repository)
		}
		if src.Package != nil {
			detail.Packages = []ServerPackage{packageOf(src.Package)}
		}
	}
	if s.Spec.Remote != nil {
		detail.Remotes = []ServerTransport{remoteOf(s.Spec.Remote)}
	}
	return ServerResponse{
		Server: detail,
		Meta:   &ResponseMeta{Official: officialMetaOf(s)},
	}
}

// descriptionOf returns a non-empty description. The spec marks description as
// required, so fall back to the title and then a generated string when the
// source resource left it blank.
func descriptionOf(s *v1alpha1.MCPServer) string {
	if s.Spec.Description != "" {
		return s.Spec.Description
	}
	if s.Spec.Title != "" {
		return s.Spec.Title
	}
	return "MCP server " + s.Metadata.Name
}

// versionOf derives the server-level version. Package origins carry the most
// precise value; fall back to a non-"latest" tag and finally a placeholder so
// the required version field is never empty.
func versionOf(s *v1alpha1.MCPServer) string {
	if src := s.Spec.Source; src != nil && src.Package != nil {
		if v := packageVersionOf(src.Package); v != "" {
			return v
		}
	}
	if t := s.Metadata.Tag; t != "" && t != "latest" {
		return t
	}
	return defaultVersion
}

// packageVersionOf extracts the concrete package version. npm/pypi carry an
// explicit version; oci encodes it in the identifier (":tag" or "@sha256:...").
func packageVersionOf(p *v1alpha1.MCPPackage) string {
	switch p.Origin.Type {
	case v1alpha1.MCPPackageOriginTypeNPM:
		if p.Origin.NPM != nil {
			return p.Origin.NPM.Version
		}
	case v1alpha1.MCPPackageOriginTypePyPI:
		if p.Origin.PyPI != nil {
			return p.Origin.PyPI.Version
		}
	case v1alpha1.MCPPackageOriginTypeOCI:
		return ociVersionFromIdentifier(p.Origin.Identifier)
	}
	return ""
}

// ociVersionFromIdentifier pulls the version out of an OCI reference. A digest
// ("repo@sha256:...") wins; otherwise the tag after the final ":" in the last
// path segment is used. Returns "" when neither is present.
func ociVersionFromIdentifier(identifier string) string {
	if at := strings.LastIndex(identifier, "@"); at != -1 {
		return identifier[at+1:]
	}
	// Only treat a ":" in the final path segment as a tag so registry ports
	// like "ghcr.io:443/foo/bar" aren't mistaken for tags.
	lastSlash := strings.LastIndex(identifier, "/")
	lastSegment := identifier[lastSlash+1:]
	if colon := strings.LastIndex(lastSegment, ":"); colon != -1 {
		return lastSegment[colon+1:]
	}
	return ""
}

// repositoryOf maps the source repository, inferring the well-known `source`
// host from the URL. Branch/commit have no v0.1 representation and are dropped.
func repositoryOf(r *v1alpha1.Repository) *ServerRepository {
	return &ServerRepository{
		URL:       r.URL,
		Source:    sourceFromURL(r.URL),
		Subfolder: r.Subfolder,
	}
}

// sourceFromURL maps a repository URL to the upstream `source` enum value for
// the common forges. Unknown hosts yield "" (the field is optional).
func sourceFromURL(url string) string {
	switch {
	case strings.Contains(url, "github.com"):
		return "github"
	case strings.Contains(url, "gitlab.com"):
		return "gitlab"
	case strings.Contains(url, "bitbucket.org"):
		return "bitbucket"
	default:
		return ""
	}
}

// packageOf maps a v1alpha1 MCPPackage (Origin/Launch/Transport) onto the
// flat upstream Package shape.
func packageOf(p *v1alpha1.MCPPackage) ServerPackage {
	out := ServerPackage{
		RegistryType:    string(p.Origin.Type),
		Identifier:      p.Origin.Identifier,
		Version:         packageVersionForWire(p),
		RegistryBaseURL: mirrorOf(p.Origin),
		Transport:       packageTransportOf(p.Transport),
	}
	if p.Launch != nil {
		// v1alpha1 has a single argument list; the launch Command maps to the
		// runtime hint. Upstream splits runtime vs package arguments, but the
		// source model doesn't, so everything lands in packageArguments.
		out.RuntimeHint = p.Launch.Command
		out.PackageArguments = argumentsOf(p.Launch.Args)
		out.EnvironmentVariables = envOf(p.Launch.Env)
	}
	return out
}

// packageVersionForWire is packageVersionOf with a placeholder fallback: the
// upstream package.version field is required and must be a specific version.
func packageVersionForWire(p *v1alpha1.MCPPackage) string {
	if v := packageVersionOf(p); v != "" {
		return v
	}
	return defaultVersion
}

// mirrorOf surfaces a configured registry mirror as the upstream
// registryBaseUrl. OCI has no mirror field in the source model.
func mirrorOf(o v1alpha1.MCPPackageOrigin) string {
	switch o.Type {
	case v1alpha1.MCPPackageOriginTypeNPM:
		if o.NPM != nil {
			return o.NPM.Mirror
		}
	case v1alpha1.MCPPackageOriginTypePyPI:
		if o.PyPI != nil {
			return o.PyPI.Mirror
		}
	}
	return ""
}

// packageTransportOf maps the deployable package transport. AgentRegistry's
// "http" becomes the upstream "streamable-http" with a synthesized localhost
// URL built from the listen port/path; "stdio" (and the empty default) map to
// the stdio transport.
func packageTransportOf(t v1alpha1.MCPTransport) ServerTransport {
	if t.Type == "http" {
		return ServerTransport{
			Type: "streamable-http",
			URL:  fmt.Sprintf("http://localhost:%d%s", t.Port, t.Path),
		}
	}
	return ServerTransport{Type: "stdio"}
}

// argumentsOf maps launch arguments. Returns nil for an empty input so the
// field is omitted from the wire.
func argumentsOf(args []v1alpha1.MCPArgument) []ServerArgument {
	if len(args) == 0 {
		return nil
	}
	out := make([]ServerArgument, 0, len(args))
	for _, a := range args {
		out = append(out, ServerArgument{
			Type:  string(a.Type),
			Name:  a.Name,
			Value: a.Value,
		})
	}
	return out
}

// envOf maps declared environment variables.
func envOf(env []v1alpha1.MCPKeyValueInput) []ServerInput {
	if len(env) == 0 {
		return nil
	}
	out := make([]ServerInput, 0, len(env))
	for _, e := range env {
		out = append(out, ServerInput{
			Name:       e.Name,
			Value:      e.Value,
			IsRequired: e.IsRequired,
		})
	}
	return out
}

// remoteOf maps a pre-running remote MCP server to a remote transport entry.
func remoteOf(r *v1alpha1.MCPRemote) ServerTransport {
	return ServerTransport{
		Type:    normalizeRemoteType(r.Type),
		URL:     r.URL,
		Headers: headersOf(r.Headers),
	}
}

// normalizeRemoteType maps source remote types onto the upstream enum. Anything
// that isn't recognizably SSE is treated as streamable-http (the modern
// default) so a remote always carries a valid transport type.
func normalizeRemoteType(t string) string {
	switch strings.ToLower(t) {
	case "sse":
		return "sse"
	default:
		return "streamable-http"
	}
}

// headersOf maps static remote headers to key/value inputs.
func headersOf(headers []v1alpha1.HTTPHeader) []ServerInput {
	if len(headers) == 0 {
		return nil
	}
	out := make([]ServerInput, 0, len(headers))
	for _, h := range headers {
		out = append(out, ServerInput{Name: h.Name, Value: h.Value})
	}
	return out
}

// officialMetaOf builds the registry-managed _meta block. isLatest reflects
// whether this row is the literal "latest" tag (the default list serves only
// that tag; the versions endpoints can surface pinned older tags too). Status
// reflects soft-deletion.
func officialMetaOf(s *v1alpha1.MCPServer) *OfficialMeta {
	tag := s.Metadata.Tag
	m := &OfficialMeta{Status: "active", IsLatest: tag == "" || tag == "latest"}
	if !s.Metadata.CreatedAt.IsZero() {
		m.PublishedAt = s.Metadata.CreatedAt.UTC().Format(time.RFC3339)
	}
	if !s.Metadata.UpdatedAt.IsZero() {
		m.UpdatedAt = s.Metadata.UpdatedAt.UTC().Format(time.RFC3339)
		m.StatusChangedAt = m.UpdatedAt
	}
	if s.Metadata.DeletionTimestamp != nil {
		m.Status = "deleted"
	}
	return m
}
