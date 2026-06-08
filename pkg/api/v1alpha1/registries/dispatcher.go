package registries

import (
	"context"
	"fmt"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// Dispatcher is the v1alpha1-native RegistryValidatorFunc. It fans
// (ctx, origin, objectName) out to the appropriate per-registry
// validator based on which Origin sub-struct is non-nil. An origin
// with no sub-struct set — or with the wrong sub-struct for its Type
// — returns a 400-style error.
//
// Use it directly as the v argument to obj.ValidateRegistries:
//
//	err := v1alpha1.ValidateObjectRegistries(ctx, obj, registries.Dispatcher)
//
// Callers that want to disable a subset of registries (e.g. unit
// tests, offline imports, air-gapped deployments) can wrap this with
// their own RegistryValidatorFunc that filters on origin.Type before
// delegating.
func Dispatcher(ctx context.Context, origin v1alpha1.MCPPackageOrigin, objectName string) error {
	switch {
	case origin.NPM != nil:
		return ValidateNPM(ctx, origin, objectName)
	case origin.PyPI != nil:
		return ValidatePyPI(ctx, origin, objectName)
	case origin.OCI != nil:
		return ValidateOCI(ctx, origin, objectName)
	default:
		return fmt.Errorf("MCPPackage origin: exactly one of npm/pypi/oci must be set (got Type=%q)", origin.Type)
	}
}
