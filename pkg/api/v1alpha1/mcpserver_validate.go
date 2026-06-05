package v1alpha1

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// Validate runs structural validation on the MCPServer envelope.
func (m *MCPServer) Validate() error {
	var errs FieldErrors
	errs = append(errs, ValidateObjectMeta(m.Metadata)...)
	errs = append(errs, validateMCPServerSpec(&m.Spec)...)
	if len(errs) == 0 {
		return nil
	}
	return errs
}

// validateMCPPackageName enforces the upstream MCP-ecosystem catalogue name format
// for the optional MCPPackage.ServerName field (e.g. "io.github.user/server").
// Matches the upstream modelcontextprotocol/registry server.json schema for
// the `name` field.
func validateMCPPackageName(s string) error {
	if s == "" {
		return nil // optional field
	}
	if l := len(s); l < UpstreamMCPPackageNameMinLen || l > UpstreamMCPPackageNameMaxLen {
		return fmt.Errorf("%w: serverName length must be %d-%d chars, got %d", ErrInvalidFormat, UpstreamMCPPackageNameMinLen, UpstreamMCPPackageNameMaxLen, l)
	}
	if !UpstreamMCPPackageNameRegex.MatchString(s) {
		return fmt.Errorf("%w: serverName must match upstream pattern `namespace/name` (e.g. \"io.github.user/server\"): %q", ErrInvalidFormat, s)
	}
	return nil
}

func validateMCPServerSpec(s *MCPServerSpec) FieldErrors {
	var errs FieldErrors
	errs.Append("spec.title", validateTitle(s.Title))

	// Source (bundled), Remote (pre-running MCP endpoint), and OpenAPI (a REST
	// API surfaced as MCP) are the three ways to describe an MCP server.
	// Exactly one must be set.
	set := 0
	for _, present := range []bool{s.Source != nil, s.Remote != nil, s.OpenAPI != nil} {
		if present {
			set++
		}
	}
	switch {
	case set == 0:
		errs.Append("spec", fmt.Errorf("%w: one of spec.source, spec.remote, or spec.openapi must be set", ErrRequiredField))
	case set > 1:
		errs.Append("spec", fmt.Errorf("%w: spec.source, spec.remote, and spec.openapi are mutually exclusive", ErrInvalidRef))
	case s.Source != nil:
		errs = append(errs, validateMCPServerSource(s.Source)...)
	case s.Remote != nil:
		errs = append(errs, validateMCPServerRemote(s.Remote)...)
	case s.OpenAPI != nil:
		errs = append(errs, validateMCPServerOpenAPI(s.OpenAPI)...)
	}

	return errs
}

// maxOpenAPISchemaBytes bounds the inline OpenAPI schema. The schema is
// materialized into a backing store with a hard per-object size limit, so an
// oversize schema is rejected here - at submit time, with a clear message -
// rather than surfacing later as an opaque write failure.
const maxOpenAPISchemaBytes = 900 * 1024

// validateMCPServerOpenAPI checks the OpenAPI arm: the URL must be an absolute
// http(s) base with no path/query/fragment (endpoint paths come from the
// schema), and the schema must be a non-empty, suitably-sized OpenAPI 3.0 JSON
// document.
func validateMCPServerOpenAPI(o *MCPServerOpenAPI) FieldErrors {
	var errs FieldErrors
	if o.URL == "" {
		errs.Append("spec.openapi.url", fmt.Errorf("%w", ErrRequiredField))
	} else if err := validateOpenAPIBaseURL(o.URL); err != nil {
		errs.Append("spec.openapi.url", err)
	}
	if o.Schema == "" {
		errs.Append("spec.openapi.schema", fmt.Errorf("%w", ErrRequiredField))
		return errs
	}
	if len(o.Schema) > maxOpenAPISchemaBytes {
		errs.Append("spec.openapi.schema", fmt.Errorf("%w: schema exceeds the maximum supported size of %d bytes", ErrInvalidFormat, maxOpenAPISchemaBytes))
		return errs
	}
	if err := validateOpenAPISchema(o.Schema); err != nil {
		errs.Append("spec.openapi.schema", err)
	}
	return errs
}

// validateOpenAPIBaseURL enforces that the REST base is an absolute http(s)
// URL carrying only scheme + host[:port]. A path, query, or fragment is
// rejected because endpoint paths are taken from the schema.
func validateOpenAPIBaseURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidURL, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("%w: scheme must be http or https", ErrInvalidURL)
	}
	// Hostname() strips any port, so an empty result rejects bases like
	// "http://:8080" that carry a port but no host. A port is optional:
	// both "https://host" and "https://host:8443" are accepted.
	if parsed.Hostname() == "" {
		return fmt.Errorf("%w: host is empty", ErrInvalidURL)
	}
	if parsed.User != nil {
		return fmt.Errorf("%w: must not include userinfo", ErrInvalidURL)
	}
	if (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("%w: must not include a path, query, or fragment", ErrInvalidURL)
	}
	return nil
}

// validateOpenAPISchema parses the inline schema as JSON and confirms it is an
// OpenAPI 3.0 document (top-level "openapi" key). Deeper structural validation
// is left to the platform serving the registry.
func validateOpenAPISchema(schema string) error {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(schema), &doc); err != nil {
		return fmt.Errorf("%w: schema must be valid JSON: %v", ErrInvalidFormat, err)
	}
	raw, ok := doc["openapi"]
	if !ok {
		return fmt.Errorf("%w: schema must be an OpenAPI 3.0 document (missing top-level \"openapi\" field)", ErrInvalidFormat)
	}
	// The "openapi" field must be a version string (e.g. "3.0.0", "3.1.0"); a
	// non-string or a non-3.x value (e.g. 2 or "2.0") is not an OpenAPI 3.x
	// document. Deeper structural validation is left to the platform serving
	// the registry.
	var version string
	if err := json.Unmarshal(raw, &version); err != nil {
		return fmt.Errorf("%w: schema \"openapi\" field must be a string", ErrInvalidFormat)
	}
	if !strings.HasPrefix(version, "3.") {
		return fmt.Errorf("%w: schema must be an OpenAPI 3.x document, got version %q", ErrInvalidFormat, version)
	}
	return nil
}

func validateMCPServerRemote(t *MCPRemote) FieldErrors {
	var errs FieldErrors
	if t.Type == "" {
		errs.Append("spec.remote.type", fmt.Errorf("%w", ErrRequiredField))
	}
	if t.URL == "" {
		errs.Append("spec.remote.url", fmt.Errorf("%w", ErrRequiredField))
		return errs
	}
	if err := validateWebsiteURL(t.URL); err != nil {
		errs.Append("spec.remote.url", err)
	}
	return errs
}

func validateMCPServerSource(src *MCPServerSource) FieldErrors {
	var errs FieldErrors
	for _, e := range validateRepository(src.Repository) {
		errs.Append("spec.source."+e.Path, e.Cause)
	}
	pkg := src.Package
	if pkg == nil {
		return errs
	}
	if pkg.RegistryType == "" {
		errs.Append("spec.source.package.registryType", fmt.Errorf("%w", ErrRequiredField))
	}
	if pkg.Identifier == "" {
		errs.Append("spec.source.package.identifier", fmt.Errorf("%w", ErrRequiredField))
	}
	if pkg.Transport.Type == "" {
		errs.Append("spec.source.package.transport.type", fmt.Errorf("%w", ErrRequiredField))
	}
	if pkg.Transport.Type == "http" && pkg.Transport.Port == 0 {
		errs.Append("spec.source.package.transport.port", fmt.Errorf("%w: required for http transport", ErrRequiredField))
	}
	// MCPB has no ownership concept (the validator ignores serverName), so it's
	// the only registry type where omitting serverName is allowed.
	if pkg.RegistryType != "" && pkg.RegistryType != RegistryTypeMCPB && pkg.ServerName == "" {
		errs.Append("spec.source.package.serverName",
			fmt.Errorf("%w: required when registryType is %q", ErrRequiredField, pkg.RegistryType))
	}
	if err := validateMCPPackageName(pkg.ServerName); err != nil {
		errs.Append("spec.source.package.serverName", err)
	}
	return errs
}
