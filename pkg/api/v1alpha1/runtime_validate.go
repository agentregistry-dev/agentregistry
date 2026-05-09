package v1alpha1

import (
	"fmt"
	"strings"
)

// KnownRuntimeTypes is the set of canonical Runtime spec.type values
// the generic validator recognizes. Keys are stored in their canonical
// CamelCase form. Validate() does the case-insensitive admission match
// against this set and rewrites Spec.Type to the canonical form, so
// downstream code can compare Spec.Type against the constants with
// exact-match equality. Downstream builds may register additional
// canonical values at init by inserting into this map.
var KnownRuntimeTypes = map[string]struct{}{
	TypeLocal:      {},
	TypeKubernetes: {},
}

// Validate runs Runtime's structural checks and canonicalizes
// Spec.Type to its CamelCase form.
//
// Manifests may write Spec.Type in any casing (`local`, `LOCAL`, `Local`)
// for ergonomic UX; the validator looks the input up in
// KnownRuntimeTypes case-insensitively and rewrites Spec.Type in place
// to the canonical CamelCase value. Every consumer downstream of
// Validate (adapter dispatch, status messages, storage) compares
// Spec.Type with exact-match equality, so case-insensitivity lives in
// exactly one place.
//
// Runtime is unversioned: a connection handle to one execution
// target (an AWS account + role, a kagent cluster, a local daemon).
// Multiple coexisting versions of the same (namespace, name) carry no
// meaning — there is no "v1" vs "v2" of the same AWS role — so the
// (namespace, name) pair is the identity.
func (r *Runtime) Validate() error {
	var errs FieldErrors
	errs = append(errs, ValidateObjectMeta(r.Metadata)...)
	if r.Spec.Type == "" {
		errs.Append("spec.type", fmt.Errorf("%w", ErrRequiredField))
	} else if canonical, ok := canonicalRuntimeType(r.Spec.Type); ok {
		r.Spec.Type = canonical
	} else {
		errs.Append("spec.type",
			fmt.Errorf("%w: %q (known: %v)", ErrUnknownRuntimeType, r.Spec.Type, knownRuntimeTypeNames()))
	}
	if len(errs) == 0 {
		return nil
	}
	return errs
}

// canonicalRuntimeType case-insensitively resolves a user-supplied
// spec.type string to its canonical CamelCase form. Returns the
// canonical value and true on a match, "" and false if no registered
// runtime type matches (case-insensitively).
func canonicalRuntimeType(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	for canonical := range KnownRuntimeTypes {
		if strings.EqualFold(s, canonical) {
			return canonical, true
		}
	}
	return "", false
}

func knownRuntimeTypeNames() []string {
	out := make([]string, 0, len(KnownRuntimeTypes))
	for k := range KnownRuntimeTypes {
		out = append(out, k)
	}
	return out
}
