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
	for i, ref := range a.Spec.MCPServers {
		ref.Kind = KindMCPServer // normalize so the resolver always sees the expected kind
		if ref.Namespace == "" {
			ref.Namespace = a.Metadata.Namespace
		}
		errs = append(errs, resolveRefWith(ctx, resolver, ref, fmt.Sprintf("spec.mcpServers[%d]", i))...)
	}
	for i, ref := range a.Spec.Skills {
		ref.Kind = KindSkill
		if ref.Namespace == "" {
			ref.Namespace = a.Metadata.Namespace
		}
		errs = append(errs, resolveRefWith(ctx, resolver, ref, fmt.Sprintf("spec.skills[%d]", i))...)
	}
	for i, ref := range a.Spec.Prompts {
		ref.Kind = KindPrompt
		if ref.Namespace == "" {
			ref.Namespace = a.Metadata.Namespace
		}
		errs = append(errs, resolveRefWith(ctx, resolver, ref, fmt.Sprintf("spec.prompts[%d]", i))...)
	}
	if len(errs) == 0 {
		return nil
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
	for i, ref := range s.MCPServers {
		// References within Agent.Spec default Kind=MCPServer; reject
		// mismatches to catch authoring mistakes early.
		kind := ref.Kind
		if kind == "" {
			kind = KindMCPServer
		}
		if kind != KindMCPServer {
			errs.Append(fmt.Sprintf("spec.mcpServers[%d].kind", i),
				fmt.Errorf("%w: must be %q, got %q", ErrInvalidRef, KindMCPServer, ref.Kind))
		}
		for _, e := range validateRef(ref) {
			errs.Append(fmt.Sprintf("spec.mcpServers[%d].%s", i, e.Path), e.Cause)
		}
	}
	for i, ref := range s.Skills {
		kind := ref.Kind
		if kind == "" {
			kind = KindSkill
		}
		if kind != KindSkill {
			errs.Append(fmt.Sprintf("spec.skills[%d].kind", i),
				fmt.Errorf("%w: must be %q, got %q", ErrInvalidRef, KindSkill, ref.Kind))
		}
		for _, e := range validateRef(ref) {
			errs.Append(fmt.Sprintf("spec.skills[%d].%s", i, e.Path), e.Cause)
		}
	}
	for i, ref := range s.Prompts {
		kind := ref.Kind
		if kind == "" {
			kind = KindPrompt
		}
		if kind != KindPrompt {
			errs.Append(fmt.Sprintf("spec.prompts[%d].kind", i),
				fmt.Errorf("%w: must be %q, got %q", ErrInvalidRef, KindPrompt, ref.Kind))
		}
		for _, e := range validateRef(ref) {
			errs.Append(fmt.Sprintf("spec.prompts[%d].%s", i, e.Path), e.Cause)
		}
	}

	return errs
}
