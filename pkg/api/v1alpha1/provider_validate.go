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
// Provider is infra/config — it lives alongside Deployment, not in
// the tagged-artifact set. Its spec describes a connection handle
// to one execution target: platform identifier, platform-specific
// config, and an optional telemetry endpoint. (namespace, name) is
// the identity; metadata.version goes into the legacy 3-tuple PK and
// is pinned to a constant via DefaultMetadataVersion below.
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

// DefaultMetadataVersion satisfies MetadataVersionDefaulter so YAML
// manifests for Provider can omit metadata.version. The constant "1"
// goes into the (namespace, name, version) PK; multi-version Provider
// is not a concept we expose. (The bundled SQL seed inserts under
// "v1" for legacy reasons — both rows coexist harmlessly under a
// different version key.)
func (p *Provider) DefaultMetadataVersion() string { return "1" }

func knownPlatformNames() []string {
	out := make([]string, 0, len(KnownPlatforms))
	for k := range KnownPlatforms {
		out = append(out, k)
	}
	return out
}
