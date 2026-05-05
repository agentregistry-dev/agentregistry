package v1alpha1

import (
	"context"
	"fmt"
	"strconv"
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
	errs = append(errs, ValidateObjectMeta(d.Metadata)...)
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

	// TargetRef: required. Accepts the bundled lifecycle kinds (Agent,
	// MCPServer) and the pre-deployed MCP peer (RemoteMCPServer).
	// Adapters dispatch on Kind: bundled goes through container/process
	// lifecycle; remote variants do thin pass-through registration.
	for _, e := range validateRef(s.TargetRef, KindAgent, KindMCPServer, KindRemoteMCPServer) {
		errs.Append("spec.targetRef."+e.Path, e.Cause)
	}
	// ProviderRef: required, must name a Provider.
	for _, e := range validateRef(s.ProviderRef, KindProvider) {
		errs.Append("spec.providerRef."+e.Path, e.Cause)
	}

	// Deployments pin to a concrete versioned target. The referenced
	// Agent/MCPServer/RemoteMCPServer rows live under integer versions;
	// an empty version (resolves to latest at reconcile time) or a
	// string-semver value (won't parse against the integer storage
	// column) reintroduces the silent-drift the versioned-resource
	// redesign exists to eliminate. ProviderRef is exempt — Provider is
	// infra/config (legacy storage shape) and uses string versions.
	// Cross-references between non-Deployment kinds (Agent.spec.mcpServers,
	// etc.) stay intentionally lenient — empty version there means "use
	// latest at lookup time" and is acceptable for those kinds.
	if err := validateIntegerVersion("spec.targetRef", s.TargetRef.Version); err != nil {
		errs = append(errs, *err)
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

// validateIntegerVersion enforces that the version on a Deployment's
// cross-reference is a non-empty positive integer. Versioned-artifact
// kinds (Agent, MCPServer, RemoteMCPServer) are stored under integer
// versions; a Deployment that wants stable pinning must reference them
// by their concrete integer version. Empty strings (would resolve to
// "latest" at reconcile time) and string semver (e.g. "v1.0.0") are
// rejected because both reintroduce the silent drift the
// immutable-resource-versioning redesign exists to eliminate.
//
// Returns nil on success, or a *FieldError pointing at field+".version"
// the caller can append directly to its FieldErrors slice.
func validateIntegerVersion(field, version string) *FieldError {
	if version == "" {
		return &FieldError{
			Path:  field + ".version",
			Cause: fmt.Errorf("%w: required (positive integer)", ErrRequiredField),
		}
	}
	n, err := strconv.Atoi(version)
	if err != nil || n <= 0 {
		return &FieldError{
			Path:  field + ".version",
			Cause: fmt.Errorf("%w: must be a positive integer, got %q", ErrInvalidVersion, version),
		}
	}
	return nil
}
