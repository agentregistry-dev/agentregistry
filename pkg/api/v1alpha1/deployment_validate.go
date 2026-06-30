package v1alpha1

import (
	"context"
	"fmt"
	"strings"
)

// Validate runs Deployment's structural checks.
//
// Deployment is unversioned: it's a runtime binding ("deploy resource X to
// runtime Y"). The thing being deployed carries its own tag via
// spec.targetRef.tag; when that tag is omitted, reference resolution uses the
// literal "latest" tag. Deployment's public identity is (namespace, name).
func (d *Deployment) Validate() error {
	var errs FieldErrors
	errs = append(errs, ValidateObjectMeta(d.Metadata)...)
	errs = append(errs, validateDeploymentSpec(&d.Spec)...)
	if len(errs) == 0 {
		return nil
	}
	return errs
}

// ResolveRefs checks that TargetRef, RuntimeRef, and every entry in
// DeploymentRefs resolve. The referenced objects must live in the
// referenced namespace; when ref.Namespace is blank on the wire we
// inherit the Deployment's own namespace (mirroring how kubectl treats
// blank metadata.namespace).
func (d *Deployment) ResolveRefs(ctx context.Context, resolver ResolverFunc) error {
	if resolver == nil {
		return nil
	}
	var errs FieldErrors

	target := d.Spec.TargetRef
	if target.Namespace == "" {
		target.Namespace = d.Metadata.Namespace
	}
	errs = append(errs, resolveRefWith(ctx, resolver, target, "spec.targetRef")...)

	runtime := d.Spec.RuntimeRef
	if runtime.Namespace == "" {
		runtime.Namespace = d.Metadata.Namespace
	}
	errs = append(errs, resolveRefWith(ctx, resolver, runtime, "spec.runtimeRef")...)

	for i, ref := range d.Spec.DeploymentRefs {
		probe := ResourceRef{Kind: KindDeployment, Namespace: ref.Namespace, Name: ref.Name}
		if probe.Namespace == "" {
			probe.Namespace = d.Metadata.Namespace
		}
		errs = append(errs, resolveRefWith(ctx, resolver, probe, fmt.Sprintf("spec.deploymentRefs[%d]", i))...)
	}

	if len(errs) == 0 {
		return nil
	}
	return errs
}

func validateDeploymentSpec(s *DeploymentSpec) FieldErrors {
	var errs FieldErrors

	// TargetRef: required. Accepts the bundled lifecycle kinds (Agent,
	// MCPServer). MCPServer covers both bundled (spec.source) and remote
	// (spec.remote) variants under a single kind; adapters dispatch on
	// whether Spec.Source or Spec.Remote is set.
	for _, e := range validateRef(s.TargetRef, KindAgent, KindMCPServer) {
		errs.Append("spec.targetRef."+e.Path, e.Cause)
	}
	// RuntimeRef: required, must name a Runtime.
	for _, e := range validateRef(s.RuntimeRef, KindRuntime) {
		errs.Append("spec.runtimeRef."+e.Path, e.Cause)
	}

	if s.TargetRef.Tag != "" {
		if err := validateTag(s.TargetRef.Tag); err != nil {
			errs.Append("spec.targetRef.tag", err)
		}
	}
	if s.Harness != nil {
		if s.TargetRef.Kind != KindAgent {
			errs.Append("spec.harness", fmt.Errorf("%w: harness selection is only valid for Agent deployments", ErrInvalidFormat))
		}
		if strings.TrimSpace(s.Harness.Type) == "" {
			errs.Append("spec.harness.type", fmt.Errorf("%w", ErrRequiredField))
		}
	}

	switch s.DesiredState {
	case "", DesiredStateDeployed, DesiredStateUndeployed:
		// Empty is allowed — defaults to "deployed" at apply-time.
	default:
		errs.Append("spec.desiredState",
			fmt.Errorf("%w: %q (expected %q or %q)",
				ErrInvalidDesiredState, s.DesiredState,
				DesiredStateDeployed, DesiredStateUndeployed))
	}

	for i, ref := range s.DeploymentRefs {
		path := fmt.Sprintf("spec.deploymentRefs[%d]", i)
		if err := validateNameField(ref.Name); err != nil {
			errs.Append(path+".name", err)
		}
		if ref.Namespace != "" && !namespaceRegex.MatchString(ref.Namespace) {
			errs.Append(path+".namespace", fmt.Errorf("%w: %q", ErrInvalidFormat, ref.Namespace))
		}
	}

	return errs
}
