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

	// Source is required: it is the pointer the controller resolves and pins.
	if s.Source == nil {
		errs.Append("spec.source", fmt.Errorf("%w", ErrRequiredField))
	} else {
		for _, e := range validatePluginSource(s.Source) {
			errs.Append("spec.source."+e.Path, e.Cause)
		}
	}
	return errs
}

// isFullCommitSHA reports whether s is a full 40-character hex commit SHA.
func isFullCommitSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}
	return true
}

func validatePluginSource(o *PluginSource) FieldErrors {
	var errs FieldErrors
	switch o.Type {
	case PluginSourceTypeGit:
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
		// A branch/tag OR a commit may be supplied (empty => the remote default
		// branch); the controller resolves whatever ref is given to a concrete
		// commit SHA and records that immutable pin in status.ResolvedSource.
		// Reject both-set (ambiguous), and require a full 40-hex SHA when Commit
		// is used — a short/non-SHA commit would never resolve and would retry
		// forever.
		if o.Git.Repository.Branch != "" && o.Git.Repository.Commit != "" {
			errs.Append("git.repository", fmt.Errorf("%w: set at most one of branch or commit", ErrInvalidFormat))
		}
		if o.Git.Repository.Commit != "" && !isFullCommitSHA(o.Git.Repository.Commit) {
			errs.Append("git.repository.commit", fmt.Errorf("%w: commit must be a full 40-character SHA (use branch for a tag/branch ref)", ErrInvalidFormat))
		}
	case PluginSourceTypeOCI:
		if o.Git != nil {
			errs.Append("git", fmt.Errorf("%w: git must be empty when type=oci", ErrInvalidFormat))
		}
		if o.OCI == nil || o.OCI.Reference == "" {
			errs.Append("oci.reference", fmt.Errorf("%w", ErrRequiredField))
			break
		}
		// Pin requirement: OCI source must be digest-pinned, not a floating tag.
		if !strings.Contains(o.OCI.Reference, "@sha256:") {
			errs.Append("oci.reference", fmt.Errorf("%w: oci source must be digest-pinned (…@sha256:…)", ErrInvalidFormat))
		}
	case "":
		errs.Append("type", fmt.Errorf("%w", ErrRequiredField))
	default:
		errs.Append("type", fmt.Errorf("%w: unknown plugin source type %q", ErrInvalidFormat, o.Type))
	}
	return errs
}
