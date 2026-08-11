package v1alpha1

import (
	"context"
	"fmt"

	"k8s.io/utils/ptr"
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
	errs = append(errs, resolveResourceRefs(ctx, resolver, ns, "spec.mcpServers", a.Spec.MCPServers, KindMCPServer)...)
	errs = append(errs, resolveResourceRefs(ctx, resolver, ns, "spec.plugins", a.Spec.Plugins, KindPlugin)...)
	errs = append(errs, resolveResourceRefs(ctx, resolver, ns, "spec.skills", a.Spec.Skills, KindSkill)...)
	if a.Spec.Instructions != nil {
		errs = append(errs, resolveResourceRefs(ctx, resolver, ns, "spec.instructions", []ResourceRef{*a.Spec.Instructions}, KindPrompt)...)
	}
	if len(errs) == 0 {
		return nil
	}
	return errs
}

// resolveResourceRefs resolves a slice of resource refs, defaulting Kind to
// defaultKind and Namespace to the agent's namespace before each lookup.
func resolveResourceRefs(ctx context.Context, resolver ResolverFunc, ns, path string, refs []ResourceRef, defaultKind string) FieldErrors {
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
	errs.Append("spec.iconUrl", validateIconURL(s.IconURL))
	if s.Source != nil {
		for _, e := range validateRepository(s.Source.Repository) {
			errs.Append("spec.source."+e.Path, e.Cause)
		}
		if s.Source.Protocol != nil {
			protocol := ptr.Deref(s.Source.Protocol, AgentProtocol(""))
			switch protocol {
			case AgentProtocolA2A, AgentProtocolHTTP:
			default:
				errs.Append("spec.source.protocol", fmt.Errorf("%w: must be %q or %q, got %q", ErrInvalidFormat, AgentProtocolA2A, AgentProtocolHTTP, protocol))
			}
		}
	}
	errs = append(errs, validateHarnessCompatibility(s.CompatibleHarnesses)...)

	// Composition refs default their Kind IN PLACE — the deploy-time resolver
	// does no defaulting, so the persisted ref must carry the kind. MCPServers
	// are available to any MCP-capable runtime; plugins/skills/instructions are
	// harness composition inputs and are gated below.
	errs = append(errs, validateResourceRefs("spec.mcpServers", s.MCPServers, KindMCPServer)...)
	errs = append(errs, validateResourceRefs("spec.plugins", s.Plugins, KindPlugin)...)
	errs = append(errs, validateResourceRefs("spec.skills", s.Skills, KindSkill)...)
	if s.Instructions != nil {
		if s.Instructions.Kind == "" {
			s.Instructions.Kind = KindPrompt
		}
		errs = append(errs, validateResourceRefs("spec.instructions", []ResourceRef{*s.Instructions}, KindPrompt)...)
	}

	// Plugins require harness composition support: without compatibleHarnesses,
	// a prebuilt Image cannot consume an injected plugin any more than a
	// harness-less Agent can. Skills/instructions relax this for Image agents:
	// the deploy path can deliver them to an image agent directly as an Agent
	// Plugins directory, so they only require compatibleHarnesses when there's
	// no source image to deliver them through.
	hasHarnessCompat := len(s.CompatibleHarnesses) > 0
	hasSource := s.Source != nil && (s.Source.Image != "" || s.Source.Repository != nil)
	if len(s.Plugins) > 0 && !hasHarnessCompat {
		errs.Append("spec", fmt.Errorf("%w: plugins require compatibleHarnesses", ErrInvalidFormat))
	}
	if (len(s.Skills) > 0 || s.Instructions != nil) && !hasHarnessCompat && !hasSource {
		errs.Append("spec", fmt.Errorf("%w: skills/instructions require compatibleHarnesses or source", ErrInvalidFormat))
	}

	return errs
}

func validateHarnessCompatibility(harnesses []HarnessCompatibility) FieldErrors {
	var errs FieldErrors
	seen := map[string]struct{}{}
	for i, harness := range harnesses {
		path := fmt.Sprintf("spec.compatibleHarnesses[%d]", i)
		if harness.Type == "" {
			errs.Append(path+".type", fmt.Errorf("%w", ErrRequiredField))
			continue
		}
		if _, ok := seen[harness.Type]; ok {
			errs.Append(path+".type", fmt.Errorf("%w: duplicate harness type %q", ErrInvalidFormat, harness.Type))
			continue
		}
		seen[harness.Type] = struct{}{}
	}
	return errs
}

// validateResourceRefs validates refs and defaults an empty Kind to expectKind
// IN PLACE. The defaulting must persist into the stored spec: the deploy-time
// resolver looks up stores[ref.Kind] with no defaulting of its own, so a ref
// left with an empty Kind would resolve to no store and fail the deploy.
// Slices share their backing array, so mutating refs[i] mutates the caller's
// slice field.
func validateResourceRefs(path string, refs []ResourceRef, expectKind string) FieldErrors {
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
