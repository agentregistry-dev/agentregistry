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
	errs.Append("spec.websiteUrl", validateWebsiteURL(s.WebsiteURL))
	for _, e := range validateRepository(s.Repository) {
		errs.Append("spec."+e.Path, e.Cause)
	}

	for i, icon := range s.Icons {
		if icon.Src == "" {
			errs.Append(fmt.Sprintf("spec.icons[%d].src", i), fmt.Errorf("%w", ErrRequiredField))
			continue
		}
		if err := validateWebsiteURL(icon.Src); err != nil {
			errs.Append(fmt.Sprintf("spec.icons[%d].src", i), err)
		}
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

	for i, r := range s.Remotes {
		if r.Type == "" {
			errs.Append(fmt.Sprintf("spec.remotes[%d].type", i), fmt.Errorf("%w", ErrRequiredField))
		}
		// Remote URL required for remote transports; stdio packages
		// shouldn't appear as remotes.
		if r.URL == "" {
			errs.Append(fmt.Sprintf("spec.remotes[%d].url", i), fmt.Errorf("%w", ErrRequiredField))
			continue
		}
		if err := validateWebsiteURL(r.URL); err != nil {
			errs.Append(fmt.Sprintf("spec.remotes[%d].url", i), err)
		}
	}

	return errs
}
