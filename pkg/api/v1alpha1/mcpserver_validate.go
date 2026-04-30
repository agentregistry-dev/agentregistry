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
	for _, e := range validateRepository(s.Repository) {
		errs.Append("spec."+e.Path, e.Cause)
	}

	for i, pkg := range s.Packages {
		if pkg.RegistryType == "" {
			errs.Append(fmt.Sprintf("spec.packages[%d].registryType", i), fmt.Errorf("%w", ErrRequiredField))
		}
		if pkg.Identifier == "" {
			errs.Append(fmt.Sprintf("spec.packages[%d].identifier", i), fmt.Errorf("%w", ErrRequiredField))
		}
		if pkg.Transport.Type == "" {
			errs.Append(fmt.Sprintf("spec.packages[%d].transport.type", i), fmt.Errorf("%w", ErrRequiredField))
		}
	}

	return errs
}
