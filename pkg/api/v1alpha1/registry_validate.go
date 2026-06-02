package v1alpha1

import (
	"context"
	"fmt"
)

// RegistryValidatorFunc validates a single package's origin against
// its referenced external registry. Implementations fan out by which
// sub-struct (Origin.NPM/PyPI/OCI) is non-nil to the appropriate
// per-registry validator. objectName is the resource's metadata.name,
// passed through to ownership-annotation checks (e.g. OCI's
// io.modelcontextprotocol.server.name label match).
//
// A nil RegistryValidatorFunc is a no-op on the ValidateRegistries
// methods; callers that aren't wired with a dispatcher skip the
// check.
type RegistryValidatorFunc func(ctx context.Context, origin MCPPackageOrigin, objectName string) error

// validateOrigins runs v against every element of origins,
// accumulating FieldErrors under the supplied path prefix (e.g.
// "spec.source.package.origin"). Returns nil FieldErrors when every
// validation passes — no-ops cleanly when v itself is nil.
func validateOrigins(
	ctx context.Context,
	v RegistryValidatorFunc,
	origins []MCPPackageOrigin,
	objectName, pathPrefix string,
) FieldErrors {
	if v == nil || len(origins) == 0 {
		return nil
	}
	var errs FieldErrors
	for i, o := range origins {
		if err := v(ctx, o, objectName); err != nil {
			errs.Append(fmt.Sprintf("%s[%d]", pathPrefix, i), err)
		}
	}
	return errs
}

// ValidateRegistries on *MCPServer dispatches the bundled MCPPackage's
// Origin to the caller-supplied per-registry validator.
func (m *MCPServer) ValidateRegistries(ctx context.Context, v RegistryValidatorFunc) error {
	if v == nil || m.Spec.Source == nil || m.Spec.Source.Package == nil {
		return nil
	}
	p := m.Spec.Source.Package
	errs := validateOrigins(ctx, v, []MCPPackageOrigin{p.Origin}, serverNameFromOrigin(p.Origin), "spec.source.package.origin")
	if len(errs) == 0 {
		return nil
	}
	return errs
}

// serverNameFromOrigin returns the per-type ServerName declared on
// whichever sub-struct is non-nil. Empty if Origin is malformed
// (no sub-struct set or multiple set) — the invariant validator
// surfaces that as its own error before this is consulted.
func serverNameFromOrigin(o MCPPackageOrigin) string {
	switch {
	case o.NPM != nil:
		return o.NPM.ServerName
	case o.PyPI != nil:
		return o.PyPI.ServerName
	case o.OCI != nil:
		return o.OCI.ServerName
	default:
		return ""
	}
}
