package v1alpha1

import (
	"context"
	"fmt"
)

// Validate runs structural validation on the Agent envelope: ObjectMeta
// format + Spec-level rules. No network I/O; ref existence is covered by
// ResolveRefs.
func (a *Agent) Validate() error {
	var errs FieldErrors
	errs = append(errs, ValidateObjectMeta(a.Metadata)...)
	errs = append(errs, validateAgentSpec(&a.Spec)...)
	if len(errs) == 0 {
		return nil
	}
	return errs
}

// ResolveRefs checks every ResourceRef in the Agent's spec exists by
// calling resolver. Returns nil if all refs resolve (or resolver is nil),
// otherwise a FieldErrors listing each dangling ref.
func (a *Agent) ResolveRefs(ctx context.Context, resolver ResolverFunc) error {
	if resolver == nil {
		return nil
	}
	var errs FieldErrors
	ns := a.Metadata.Namespace
	errs = append(errs, resolveHarnessRefs(ctx, resolver, ns, "spec.mcpServers", a.Spec.MCPServers, KindMCPServer)...)
	errs = append(errs, resolveHarnessRefs(ctx, resolver, ns, "spec.plugins", a.Spec.Plugins, KindPlugin)...)
	errs = append(errs, resolveHarnessRefs(ctx, resolver, ns, "spec.skills", a.Spec.Skills, KindSkill)...)
	if a.Spec.Instructions != nil {
		errs = append(errs, resolveHarnessRefs(ctx, resolver, ns, "spec.instructions", []ResourceRef{*a.Spec.Instructions}, KindPrompt)...)
	}
	if len(errs) == 0 {
		return nil
	}
	return errs
}

// resolveHarnessRefs resolves a slice of harness refs, defaulting Kind to
// defaultKind and Namespace to the agent's namespace before each lookup.
func resolveHarnessRefs(ctx context.Context, resolver ResolverFunc, ns, path string, refs []ResourceRef, defaultKind string) FieldErrors {
	var errs FieldErrors
	for i, ref := range refs {
		if ref.Kind == "" {
			ref.Kind = defaultKind
		}
		if ref.Namespace == "" {
			ref.Namespace = ns
		}
		errs = append(errs, resolveRefWith(ctx, resolver, ref, fmt.Sprintf("%s[%d]", path, i))...)
	}
	return errs
}

// validateAgentSpec runs structural checks on AgentSpec. Called by
// Agent.Validate; exported-indirectly so tests can target the spec
// directly when the envelope isn't in hand.
func validateAgentSpec(s *AgentSpec) FieldErrors {
	var errs FieldErrors

	errs.Append("spec.title", validateTitle(s.Title))
	if s.Source != nil {
		for _, e := range validateRepository(s.Source.Repository) {
			errs.Append("spec.source."+e.Path, e.Cause)
		}
	}
	// Composition refs default their Kind IN PLACE — the deploy-time resolver
	// does no defaulting, so the persisted ref must carry the kind. MCPServers
	// are available to any MCP-capable runtime; plugins/skills/instructions are
	// harness composition.
	errs = append(errs, validateHarnessRefs("spec.mcpServers", s.MCPServers, KindMCPServer)...)
	errs = append(errs, validateHarnessRefs("spec.plugins", s.Plugins, KindPlugin)...)
	errs = append(errs, validateHarnessRefs("spec.skills", s.Skills, KindSkill)...)
	if s.Instructions != nil {
		if s.Instructions.Kind == "" {
			s.Instructions.Kind = KindPrompt
		}
		errs = append(errs, validateHarnessRefs("spec.instructions", []ResourceRef{*s.Instructions}, KindPrompt)...)
	}

	// Plugins/skills/instructions only apply to harness agents — a prebuilt
	// Image cannot consume injected files.
	if (len(s.Plugins) > 0 || len(s.Skills) > 0 || s.Instructions != nil) &&
		(s.Source == nil || s.Source.Harness == nil) {
		errs.Append("spec", fmt.Errorf("%w: plugins/skills/instructions require a harness source (spec.source.harness)", ErrInvalidFormat))
	}

	if s.Source != nil && s.Source.Harness != nil {
		if s.Source.Image != "" {
			errs.Append("spec.source", fmt.Errorf("%w: harness and image are mutually exclusive", ErrInvalidFormat))
		}
		errs = append(errs, validateHarnessConfig(s.Source.Harness)...)
	}

	return errs
}

// validateHarnessConfig runs structural checks on a harness-based agent's
// source. Only the harness Type is required here; composition refs
// (plugins/skills/instructions/mcpServers) live on AgentSpec and are validated
// there.
func validateHarnessConfig(h *HarnessConfig) FieldErrors {
	var errs FieldErrors
	if h.Type == "" {
		errs.Append("spec.source.harness.type", fmt.Errorf("%w", ErrRequiredField))
	}
	return errs
}

// validateHarnessRefs validates refs and defaults an empty Kind to expectKind
// IN PLACE. The defaulting must persist into the stored spec: the deploy-time
// resolver looks up stores[ref.Kind] with no defaulting of its own, so a ref
// left with an empty Kind would resolve to no store and fail the deploy.
// Slices share their backing array, so mutating refs[i] mutates the caller's
// HarnessConfig field.
func validateHarnessRefs(path string, refs []ResourceRef, expectKind string) FieldErrors {
	var errs FieldErrors
	for i := range refs {
		if refs[i].Kind == "" {
			refs[i].Kind = expectKind
		}
		if refs[i].Kind != expectKind {
			errs.Append(fmt.Sprintf("%s[%d].kind", path, i),
				fmt.Errorf("%w: must be %q, got %q", ErrInvalidRef, expectKind, refs[i].Kind))
		}
		for _, e := range validateRef(refs[i]) {
			errs.Append(fmt.Sprintf("%s[%d].%s", path, i, e.Path), e.Cause)
		}
	}
	return errs
}
