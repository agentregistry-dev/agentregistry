package v1alpha1

import (
	"context"
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

// ResolveRefs checks every composition ref in the Plugin's spec exists by
// calling resolver. ComponentRef carries no Kind — each field supplies the
// kind it implies — so there is no defaulting or mismatch surface here.
func (p *Plugin) ResolveRefs(ctx context.Context, resolver ResolverFunc) error {
	if resolver == nil {
		return nil
	}
	var errs FieldErrors
	ns := p.Metadata.Namespace
	errs = append(errs, resolveComponentRefs(ctx, resolver, ns, "spec.skills", p.Spec.Skills, KindSkill)...)
	errs = append(errs, resolveComponentRefs(ctx, resolver, ns, "spec.mcpServers", p.Spec.MCPServers, KindMCPServer)...)
	errs = append(errs, resolveComponentRefs(ctx, resolver, ns, "spec.commands", p.Spec.Commands, KindPrompt)...)
	if p.Spec.Instructions != nil {
		errs = append(errs, resolveComponentRefs(ctx, resolver, ns, "spec.instructions", []ComponentRef{*p.Spec.Instructions}, KindPrompt)...)
	}
	if len(errs) == 0 {
		return nil
	}
	return errs
}

// resolveComponentRefs resolves a slice of component refs against the kind
// the holding field implies.
func resolveComponentRefs(ctx context.Context, resolver ResolverFunc, ns, path string, refs []ComponentRef, kind string) FieldErrors {
	var errs FieldErrors
	for i, ref := range refs {
		errs = append(errs, resolveRefWith(ctx, resolver, ref.AsResourceRef(kind, ns), fmt.Sprintf("%s[%d]", path, i))...)
	}
	return errs
}

func validatePluginSpec(s *PluginSpec) FieldErrors {
	var errs FieldErrors
	errs.Append("spec.title", validateTitle(s.Title))
	errs.Append("spec.iconUrl", validateIconURL(s.IconURL))

	// A plugin is a base source, a composition of registry artifacts, or both.
	if s.Source == nil && !s.hasComposition() {
		errs.Append("spec", fmt.Errorf("%w: set source and/or composition fields (skills/mcpServers/commands/instructions)", ErrRequiredField))
	}
	if s.Source != nil {
		for _, e := range validatePluginSource(s.Source) {
			errs.Append("spec.source."+e.Path, e.Cause)
		}
	}

	// Materialized paths are keyed by name (skills/<name>/, commands/<name>.md),
	// so duplicate names within one field have no defined precedence — reject.
	// Overlay-vs-base collisions are legal (overlay wins) and handled at compose.
	errs = append(errs, validateComponentRefs("spec.skills", s.Skills, KindSkill, true)...)
	errs = append(errs, validateComponentRefs("spec.mcpServers", s.MCPServers, KindMCPServer, true)...)
	errs = append(errs, validateComponentRefs("spec.commands", s.Commands, KindPrompt, true)...)
	if s.Instructions != nil {
		errs = append(errs, validateComponentRefs("spec.instructions", []ComponentRef{*s.Instructions}, KindPrompt, false)...)
	}
	return errs
}

// hasComposition reports whether any composition field is set.
func (s *PluginSpec) hasComposition() bool {
	return len(s.Skills) > 0 || len(s.MCPServers) > 0 || len(s.Commands) > 0 || s.Instructions != nil
}

// validateComponentRefs runs structural checks on component refs: name/
// namespace/tag formats (via validateRef with the field's implied kind) and,
// when rejectDuplicates is set, uniqueness of (namespace-defaulted) names.
func validateComponentRefs(path string, refs []ComponentRef, kind string, rejectDuplicates bool) FieldErrors {
	var errs FieldErrors
	seen := map[string]struct{}{}
	for i, ref := range refs {
		for _, e := range validateRef(ref.AsResourceRef(kind, "")) {
			// Kind is machine-supplied here; a kind error would be a
			// programming bug, but the path stays honest either way.
			errs.Append(fmt.Sprintf("%s[%d].%s", path, i, e.Path), e.Cause)
		}
		if !rejectDuplicates {
			continue
		}
		if _, ok := seen[ref.Name]; ok {
			errs.Append(fmt.Sprintf("%s[%d].name", path, i),
				fmt.Errorf("%w: duplicate name %q (materialized path is keyed by name)", ErrInvalidFormat, ref.Name))
			continue
		}
		seen[ref.Name] = struct{}{}
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
