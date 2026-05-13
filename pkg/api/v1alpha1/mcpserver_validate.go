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
	return errs
}
