package v1alpha1

import (
	"fmt"
	"strings"
)

// KnownRuntimeTypes is the set of Runtime spec.type values the generic
// validator recognizes, keyed by lowercase canonical form so matching
// is case-insensitive. Downstream builds may register additional values
// at init by inserting into this map.
var KnownRuntimeTypes = map[string]struct{}{
	strings.ToLower(TypeLocal):      {},
	strings.ToLower(TypeKubernetes): {},
}

// Validate runs Runtime's structural checks.
//
// Runtime is unversioned: a connection handle to one execution
// target (an AWS account + role, a kagent cluster, a local daemon).
// Multiple coexisting versions of the same (namespace, name) carry no
// meaning — there is no "v1" vs "v2" of the same AWS role — so the
// (namespace, name) pair is the identity. The storage layer still
// requires a version string in its 3-tuple PK; callers pin it to a
// constant ("1") rather than fabricate semantic versions.
func (r *Runtime) Validate() error {
	var errs FieldErrors
	errs = append(errs, ValidateObjectMetaUnversioned(r.Metadata)...)
	if r.Spec.Type == "" {
		errs.Append("spec.type", fmt.Errorf("%w", ErrRequiredField))
	} else if _, ok := KnownRuntimeTypes[strings.ToLower(r.Spec.Type)]; !ok {
		errs.Append("spec.type",
			fmt.Errorf("%w: %q (known: %v)", ErrUnknownRuntimeType, r.Spec.Type, knownRuntimeTypeNames()))
	}
	if len(errs) == 0 {
		return nil
	}
	return errs
}

// DefaultMetadataVersion satisfies MetadataVersionDefaulter so YAML
// manifests for Runtime can omit metadata.version. The constant "1"
// goes into the (namespace, name, version) PK; multi-version Runtime
// is not a concept we expose.
func (r *Runtime) DefaultMetadataVersion() string { return "1" }

func knownRuntimeTypeNames() []string {
	out := make([]string, 0, len(KnownRuntimeTypes))
	for k := range KnownRuntimeTypes {
		out = append(out, k)
	}
	return out
}
