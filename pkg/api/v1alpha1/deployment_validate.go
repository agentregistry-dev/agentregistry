package v1alpha1

import (
	"context"
	"fmt"
)

// Validate runs Deployment's structural checks.
//
// Deployment is unversioned: it's a runtime binding ("deploy resource X
// to provider Y"). The thing being deployed already carries its own
// version via spec.targetRef.version; the Deployment row's own
// metadata.version doesn't track anything observable. (namespace, name)
// is the identity; callers pin metadata.version to a constant ("1").
func (d *Deployment) Validate() error {
	var errs FieldErrors
	errs = append(errs, ValidateObjectMetaUnversioned(d.Metadata)...)
	errs = append(errs, validateDeploymentSpec(&d.Spec)...)
	if len(errs) == 0 {
		return nil
	}
	return errs
}

// DefaultMetadataVersion satisfies MetadataVersionDefaulter so YAML
// manifests for Deployment can omit metadata.version. The constant
// "1" goes into the (namespace, name, version) PK; the thing being
// deployed already carries its own semantic version via
// spec.targetRef.version.
func (d *Deployment) DefaultMetadataVersion() string { return "1" }

// ResolveRefs checks that TargetRef and ProviderRef both resolve. The
// referenced objects must live in the referenced namespace; when
// ref.Namespace is blank on the wire we inherit the Deployment's own
// namespace (mirroring how kubectl treats blank metadata.namespace).
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

	provider := d.Spec.ProviderRef
	if provider.Namespace == "" {
		provider.Namespace = d.Metadata.Namespace
	}
	errs = append(errs, resolveRefWith(ctx, resolver, provider, "spec.providerRef")...)

	if len(errs) == 0 {
		return nil
	}
	return errs
}

func validateDeploymentSpec(s *DeploymentSpec) FieldErrors {
	var errs FieldErrors

	// TargetRef: required, must name an Agent or MCPServer.
	for _, e := range validateRef(s.TargetRef, KindAgent, KindMCPServer) {
		errs.Append("spec.targetRef."+e.Path, e.Cause)
	}
	// ProviderRef: required, must name a Provider.
	for _, e := range validateRef(s.ProviderRef, KindProvider) {
		errs.Append("spec.providerRef."+e.Path, e.Cause)
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

	return errs
}
