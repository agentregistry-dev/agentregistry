package v1alpha1

import "fmt"

// Validate runs structural validation on the RemoteMCPServer envelope.
func (r *RemoteMCPServer) Validate() error {
	var errs FieldErrors
	errs = append(errs, ValidateObjectMeta(r.Metadata)...)
	errs = append(errs, validateRemoteMCPServerSpec(&r.Spec)...)
	if len(errs) == 0 {
		return nil
	}
	return errs
}

func validateRemoteMCPServerSpec(s *RemoteMCPServerSpec) FieldErrors {
	var errs FieldErrors
	errs.Append("spec.title", validateTitle(s.Title))

	if s.Remote.Type == "" {
		errs.Append("spec.remote.type", fmt.Errorf("%w", ErrRequiredField))
	}
	if s.Remote.URL == "" {
		errs.Append("spec.remote.url", fmt.Errorf("%w", ErrRequiredField))
		return errs
	}
	if err := validateWebsiteURL(s.Remote.URL); err != nil {
		errs.Append("spec.remote.url", err)
	}
	return errs
}
