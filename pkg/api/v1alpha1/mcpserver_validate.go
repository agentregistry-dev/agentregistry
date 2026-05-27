package v1alpha1

import "fmt"

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
