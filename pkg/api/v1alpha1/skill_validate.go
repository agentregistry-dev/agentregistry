package v1alpha1

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
	for _, e := range validateRepository(s.Repository) {
		errs.Append("spec."+e.Path, e.Cause)
	}
	return errs
}
