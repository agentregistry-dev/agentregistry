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

	// Origin is required and must be pinned so the published tag is
	// reproducible (resolve-and-freeze at publish).
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
		// Pin requirement: a published git origin must carry a commit SHA.
		if o.Git.Repository.Commit == "" {
			errs.Append("git.repository.commit", fmt.Errorf("%w: git origin must be pinned to a commit", ErrInvalidFormat))
		}
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
