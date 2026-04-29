package v1alpha1

import "fmt"

func (s *Skill) Validate() error {
	var errs FieldErrors
	errs = append(errs, ValidateObjectMeta(s.Metadata)...)
	errs = append(errs, validateSkillSpec(&s.Spec)...)
	if len(errs) == 0 {
		return nil
	}
	return errs
}

func validateSkillSpec(s *SkillSpec) FieldErrors {
	var errs FieldErrors
	errs.Append("spec.title", validateTitle(s.Title))
	errs.Append("spec.websiteUrl", validateWebsiteURL(s.WebsiteURL))
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
		// OCI packages pin version inside identifier ("host/name:tag"); the
		// OCI registry validator rejects a separate version field, so we
		// don't require one here either.
		if pkg.RegistryType != RegistryTypeOCI && pkg.Version == "" {
			errs.Append(fmt.Sprintf("spec.packages[%d].version", i), fmt.Errorf("%w", ErrRequiredField))
		}
	}
	for i, r := range s.Remotes {
		if err := validateWebsiteURL(r.URL); err != nil {
			errs.Append(fmt.Sprintf("spec.remotes[%d].url", i), err)
		}
	}
	return errs
}
