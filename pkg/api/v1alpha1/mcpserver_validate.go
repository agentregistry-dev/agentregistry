package v1alpha1

import "fmt"

// Validate runs structural validation on the MCPServer envelope.
func (m *MCPServer) Validate() error {
	var errs FieldErrors
	// Use ObjectMeta's namespace+labels checks but replace its generic loose name
	// check with the MCP-specific DNS-1123 label rule.
	for _, e := range ValidateObjectMeta(m.Metadata) {
		if e.Path != "metadata.name" {
			errs = append(errs, e)
		}
	}
	if err := validateMCPServerName(m.Metadata.Name); err != nil {
		errs.Append("metadata.name", err)
	}
	errs = append(errs, validateMCPServerSpec(&m.Spec)...)
	if len(errs) == 0 {
		return nil
	}
	return errs
}

// validateMCPServerName enforces DNS-1123 label for MCPServer's metadata.name:
// lowercase alphanumeric and hyphens only, must start and end with alphanumeric,
// max 63 chars.
func validateMCPServerName(name string) error {
	if name == "" {
		return fmt.Errorf("%w", ErrRequiredField)
	}
	if len(name) > DNSLabelMaxLen {
		return fmt.Errorf("%w: must be DNS-1123 label (max %d chars), got %d", ErrInvalidFormat, DNSLabelMaxLen, len(name))
	}
	if !DNSLabelRegex.MatchString(name) {
		return fmt.Errorf("%w: must be DNS-1123 label (lowercase alphanumeric and hyphens; start/end with alphanumeric): %q", ErrInvalidFormat, name)
	}
	return nil
}

// validateMCPPackageName enforces the upstream MCP-ecosystem catalogue name format
// for the optional MCPPackage.MCPName field (e.g. "io.github.user/server").
// Matches the upstream modelcontextprotocol/registry server.json schema for
// the `name` field.
func validateMCPPackageName(s string) error {
	if s == "" {
		return nil // optional field
	}
	if l := len(s); l < UpstreamMCPPackageNameMinLen || l > UpstreamMCPPackageNameMaxLen {
		return fmt.Errorf("%w: mcpName length must be %d-%d chars, got %d", ErrInvalidFormat, UpstreamMCPPackageNameMinLen, UpstreamMCPPackageNameMaxLen, l)
	}
	if !UpstreamMCPPackageNameRegex.MatchString(s) {
		return fmt.Errorf("%w: mcpName must match upstream pattern `namespace/name` (e.g. \"io.github.user/server\"): %q", ErrInvalidFormat, s)
	}
	return nil
}

func validateMCPServerSpec(s *MCPServerSpec) FieldErrors {
	var errs FieldErrors
	errs.Append("spec.title", validateTitle(s.Title))

	// Source (bundled) and Remote (pre-running) are the two ways to describe
	// an MCP server. Exactly one must be set.
	switch {
	case s.Source == nil && s.Remote == nil:
		errs.Append("spec", fmt.Errorf("%w: one of spec.source or spec.remote must be set", ErrRequiredField))
	case s.Source != nil && s.Remote != nil:
		errs.Append("spec", fmt.Errorf("%w: spec.source and spec.remote are mutually exclusive", ErrInvalidRef))
	case s.Source != nil:
		errs = append(errs, validateMCPServerSource(s.Source)...)
	case s.Remote != nil:
		errs = append(errs, validateMCPServerRemote(s.Remote)...)
	}

	return errs
}

func validateMCPServerRemote(t *MCPTransport) FieldErrors {
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
	if err := validateMCPPackageName(pkg.MCPName); err != nil {
		errs.Append("spec.source.package.mcpName", err)
	}
	return errs
}
