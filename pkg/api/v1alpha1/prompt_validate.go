package v1alpha1

func (p *Prompt) Validate() error {
	var errs FieldErrors
	errs = append(errs, ValidateObjectMeta(p.Metadata)...)
	// PromptSpec has minimal structure (Description + Content + IconURL).
	// Content MAY be empty (a prompt can be purely descriptive), so we don't
	// require it here.
	errs.Append("spec.iconUrl", validateIconURL(p.Spec.IconURL))
	if len(errs) == 0 {
		return nil
	}
	return errs
}
