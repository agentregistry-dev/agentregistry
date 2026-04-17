package v1alpha1

import "context"

func (p *Prompt) Validate() error {
	var errs FieldErrors
	errs = append(errs, ValidateObjectMeta(p.Metadata)...)
	// PromptSpec has minimal structure (Description + Content). Content
	// MAY be empty (a prompt can be purely descriptive), so we don't
	// require it here.
	if len(errs) == 0 {
		return nil
	}
	return errs
}

// ResolveRefs on a Prompt is a no-op — PromptSpec holds no ResourceRefs.
func (p *Prompt) ResolveRefs(ctx context.Context, resolver ResolverFunc) error { return nil }
