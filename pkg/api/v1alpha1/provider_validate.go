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
