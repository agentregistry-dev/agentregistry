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
		if ref.Kind == "" {
			ref.Kind = KindMCPServer
		}
		if ref.Namespace == "" {
			ref.Namespace = a.Metadata.Namespace
		}
		errs = append(errs, resolveRefWith(ctx, resolver, ref, fmt.Sprintf("spec.mcpServers[%d]", i))...)
	}
	if a.Spec.Source != nil && a.Spec.Source.Harness != nil {
		h := a.Spec.Source.Harness
		errs = append(errs, resolveHarnessRefs(ctx, resolver, a.Metadata.Namespace, "spec.source.harness.plugins", h.Plugins, KindPlugin)...)
		errs = append(errs, resolveHarnessRefs(ctx, resolver, a.Metadata.Namespace, "spec.source.harness.skills", h.Skills, KindSkill)...)
		errs = append(errs, resolveHarnessRefs(ctx, resolver, a.Metadata.Namespace, "spec.source.harness.mcpServers", h.MCPServers, KindMCPServer)...)
		if h.Instructions != nil {
			errs = append(errs, resolveHarnessRefs(ctx, resolver, a.Metadata.Namespace, "spec.source.harness.instructions", []ResourceRef{*h.Instructions}, KindPrompt)...)
		}
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
	for i := range s.MCPServers {
		// References within Agent.Spec default Kind=MCPServer (covering both
		// bundled and remote servers). Default IN PLACE so the persisted ref
		// carries the kind — the deploy-time resolver does no defaulting and an
		// empty Kind would resolve to no store.
		if s.MCPServers[i].Kind == "" {
			s.MCPServers[i].Kind = KindMCPServer
		}
		if s.MCPServers[i].Kind != KindMCPServer {
			errs.Append(fmt.Sprintf("spec.mcpServers[%d].kind", i),
				fmt.Errorf("%w: must be %q, got %q",
					ErrInvalidRef, KindMCPServer, s.MCPServers[i].Kind))
		}
		for _, e := range validateRef(s.MCPServers[i]) {
			errs.Append(fmt.Sprintf("spec.mcpServers[%d].%s", i, e.Path), e.Cause)
		}
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
// source: a harness Type is required, and every referenced plugin/skill/MCP
// server/instructions ref must be well-formed and name the expected kind.
func validateHarnessConfig(h *HarnessConfig) FieldErrors {
	var errs FieldErrors
	if h.Type == "" {
		errs.Append("spec.source.harness.type", fmt.Errorf("%w", ErrRequiredField))
	}
	errs = append(errs, validateHarnessRefs("spec.source.harness.plugins", h.Plugins, KindPlugin)...)
	errs = append(errs, validateHarnessRefs("spec.source.harness.skills", h.Skills, KindSkill)...)
	errs = append(errs, validateHarnessRefs("spec.source.harness.mcpServers", h.MCPServers, KindMCPServer)...)
	if h.Instructions != nil {
		// Default in place so the persisted ref carries the kind (see below);
		// the copy passed to validateHarnessRefs then already has it.
		if h.Instructions.Kind == "" {
			h.Instructions.Kind = KindPrompt
		}
		errs = append(errs, validateHarnessRefs("spec.source.harness.instructions", []ResourceRef{*h.Instructions}, KindPrompt)...)
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
