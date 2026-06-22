package v1alpha1

import (
	"fmt"
	"strings"
)

func (p *Plugin) Validate() error {
	var errs FieldErrors
	errs = append(errs, ValidateObjectMeta(p.Metadata)...)
	errs = append(errs, validatePluginSpec(&p.Spec)...)
	if len(errs) == 0 {
		return nil
	}
	return errs
}

func validatePluginSpec(s *PluginSpec) FieldErrors {
	var errs FieldErrors
	errs.Append("spec.title", validateTitle(s.Title))

	// Origin is required: it is the pointer the controller resolves and pins.
	if s.Origin == nil {
		errs.Append("spec.origin", fmt.Errorf("%w", ErrRequiredField))
	} else {
		for _, e := range validatePluginOrigin(s.Origin) {
			errs.Append("spec.origin."+e.Path, e.Cause)
		}
	}
	return errs
}

func validatePluginOrigin(o *PluginOrigin) FieldErrors {
	var errs FieldErrors
	switch o.Type {
	case PluginOriginTypeGit:
		if o.OCI != nil {
			errs.Append("oci", fmt.Errorf("%w: oci must be empty when type=git", ErrInvalidFormat))
		}
		if o.Git == nil || o.Git.Repository == nil {
			errs.Append("git.repository", fmt.Errorf("%w", ErrRequiredField))
			break
		}
		for _, e := range validateRepository(o.Git.Repository) {
			errs.Append("git."+e.Path, e.Cause)
		}
		if o.Git.Repository.URL == "" {
			errs.Append("git.repository.url", fmt.Errorf("%w", ErrRequiredField))
		}
		// A branch, tag, or commit may be supplied (empty => the remote default
		// branch). The controller resolves whatever ref is given to a concrete
		// commit SHA and records that immutable pin in status.ResolvedSource.
	case PluginOriginTypeOCI:
		if o.Git != nil {
			errs.Append("git", fmt.Errorf("%w: git must be empty when type=oci", ErrInvalidFormat))
		}
		if o.OCI == nil || o.OCI.Reference == "" {
			errs.Append("oci.reference", fmt.Errorf("%w", ErrRequiredField))
			break
		}
		// Pin requirement: OCI origin must be digest-pinned, not a floating tag.
		if !strings.Contains(o.OCI.Reference, "@sha256:") {
			errs.Append("oci.reference", fmt.Errorf("%w: oci origin must be digest-pinned (…@sha256:…)", ErrInvalidFormat))
		}
	case "":
		errs.Append("type", fmt.Errorf("%w", ErrRequiredField))
	default:
		errs.Append("type", fmt.Errorf("%w: unknown plugin origin type %q", ErrInvalidFormat, o.Type))
	}
	return errs
}
