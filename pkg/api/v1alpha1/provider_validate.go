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
// Provider is infra/config — it lives alongside Deployment, not in the
// tagged-artifact set. Its public identity is Kubernetes-like
// (namespace, name); any private storage identity is hidden.
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

// DefaultMutableObjectIdentity satisfies MutableObjectIdentityDefaulter.
// Multi-version Provider is not a public concept.
func (p *Provider) DefaultMutableObjectIdentity() string { return "1" }

func knownPlatformNames() []string {
	out := make([]string, 0, len(KnownPlatforms))
	for k := range KnownPlatforms {
		out = append(out, k)
	}
	return out
}
