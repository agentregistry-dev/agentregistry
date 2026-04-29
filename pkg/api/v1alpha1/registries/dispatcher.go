package registries

import (
	"context"
	"fmt"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// Dispatcher is the v1alpha1-native RegistryValidatorFunc. It fans
// (ctx, pkg, objectName) out to the appropriate per-registry
// validator based on pkg.RegistryType. Unknown RegistryType values
// return a 400-style error.
//
// Use it directly as the v argument to obj.ValidateRegistries:
//
//	err := v1alpha1.ValidateObjectRegistries(ctx, obj, registries.Dispatcher)
//
// Callers that want to disable a subset of registries (e.g. unit
// tests, offline imports, air-gapped deployments) can wrap this
// with their own RegistryValidatorFunc that filters by
// pkg.RegistryType before delegating.
func Dispatcher(ctx context.Context, pkg v1alpha1.RegistryPackage, objectName string) error {
	switch pkg.RegistryType {
	case v1alpha1.RegistryTypeNPM:
		return ValidateNPM(ctx, pkg, objectName)
	case v1alpha1.RegistryTypePyPI:
		return ValidatePyPI(ctx, pkg, objectName)
	case v1alpha1.RegistryTypeNuGet:
		return ValidateNuGet(ctx, pkg, objectName)
	case v1alpha1.RegistryTypeOCI:
		return ValidateOCI(ctx, pkg, objectName)
	case v1alpha1.RegistryTypeMCPB:
		return ValidateMCPB(ctx, pkg, objectName)
	default:
		return fmt.Errorf("unsupported registry type: %s", pkg.RegistryType)
	}
}
