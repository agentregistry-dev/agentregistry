package v1alpha1

import (
	"context"
	"fmt"
)

func (s *Skill) Validate() error {
	var errs FieldErrors
	errs = append(errs, ValidateObjectMeta(s.Metadata)...)
	errs = append(errs, validateSkillSpec(&s.Spec)...)
	if len(errs) == 0 {
		return nil
	}
	return errs
}

// ResolveRefs on a Skill is a no-op — SkillSpec holds no ResourceRefs.
func (s *Skill) ResolveRefs(ctx context.Context, resolver ResolverFunc) error { return nil }

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
		if pkg.Version == "" {
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
