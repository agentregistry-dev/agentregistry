package v1alpha1

import (
	"fmt"
)

// KnownPlatforms is the set of Provider spec.platform values the generic
// validator recognizes. Enterprise platforms may register additional
// values via KnownPlatformsMutation at init.
var KnownPlatforms = map[string]struct{}{
	PlatformLocal:      {},
	PlatformKubernetes: {},
}

// Validate runs Provider's structural checks.
//
// Provider is unversioned: a connection handle to one execution
// target (an AWS account + role, a kagent cluster, a local daemon).
// Multiple coexisting versions of the same (namespace, name) carry no
// meaning — there is no "v1" vs "v2" of the same AWS role — so the
// (namespace, name) pair is the identity. The storage layer still
// requires a version string in its 3-tuple PK; callers pin it to a
// constant ("1") rather than fabricate semantic versions.
func (p *Provider) Validate() error {
	var errs FieldErrors
	errs = append(errs, ValidateObjectMeta(p.Metadata)...)
	if p.Spec.Platform == "" {
		errs.Append("spec.platform", fmt.Errorf("%w", ErrRequiredField))
	} else if _, ok := KnownPlatforms[p.Spec.Platform]; !ok {
		errs.Append("spec.platform",
			fmt.Errorf("%w: %q (known: %v)", ErrUnknownPlatform, p.Spec.Platform, knownPlatformNames()))
	}
	if len(errs) == 0 {
		return nil
	}
	return errs
}

func knownPlatformNames() []string {
	out := make([]string, 0, len(KnownPlatforms))
	for k := range KnownPlatforms {
		out = append(out, k)
	}
	return out
}
