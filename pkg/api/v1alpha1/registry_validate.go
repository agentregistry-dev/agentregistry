package v1alpha1

import (
	"context"
)

// RegistryValidatorFunc validates a single package's origin against
// its referenced external registry. Implementations fan out by which
// sub-struct (Origin.NPM/PyPI/OCI) is non-nil to the appropriate
// per-registry validator. expectedServerName is the upstream-claimed
// server identity declared on the origin's sub-struct (e.g.
// origin.oci.serverName), passed through to ownership-annotation
// checks (e.g. OCI's io.modelcontextprotocol.server.name label match).
//
// A nil RegistryValidatorFunc is a no-op on the ValidateRegistries
// methods; callers that aren't wired with a dispatcher skip the
// check.
type RegistryValidatorFunc func(ctx context.Context, origin MCPPackageOrigin, expectedServerName string) error

// ValidateRegistries on *MCPServer dispatches the bundled MCPPackage's
// Origin to the caller-supplied per-registry validator.
func (m *MCPServer) ValidateRegistries(ctx context.Context, v RegistryValidatorFunc) error {
	if v == nil || m.Spec.Source == nil || m.Spec.Source.Package == nil {
		return nil
	}
	p := m.Spec.Source.Package
	if err := v(ctx, p.Origin, serverNameFromOrigin(p.Origin)); err != nil {
		var errs FieldErrors
		errs.Append("spec.source.package.origin", err)
		return errs
	}
	return nil
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
