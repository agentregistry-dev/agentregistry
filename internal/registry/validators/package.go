package validators

import (
	"context"
	"fmt"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1/registries"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

// ValidatePackage validates that the package referenced in the server configuration is:
// 1. allowed on the official registry (based on registry base url); and
// 2. owned by the publisher, by checking for a matching server name in the package metadata
//
// All per-registry validators now live under pkg/api/v1alpha1/registries
// and operate on v1alpha1.RegistryPackage. This function preserves the
// legacy model.Package entry point (still called by the legacy
// importer + service layer) and translates on the fly; new-path
// callers use registries.Validator directly.
func ValidatePackage(ctx context.Context, pkg model.Package, serverName string) error {
	rp := v1alpha1.RegistryPackage{
		RegistryType:    pkg.RegistryType,
		Identifier:      pkg.Identifier,
		Version:         pkg.Version,
		RegistryBaseURL: pkg.RegistryBaseURL,
		FileSHA256:      pkg.FileSHA256,
	}
	switch pkg.RegistryType {
	case model.RegistryTypeNPM:
		return registries.ValidateNPM(ctx, rp, serverName)
	case model.RegistryTypePyPI:
		return registries.ValidatePyPI(ctx, rp, serverName)
	case model.RegistryTypeNuGet:
		return registries.ValidateNuGet(ctx, rp, serverName)
	case model.RegistryTypeOCI:
		return registries.ValidateOCI(ctx, rp, serverName)
	case model.RegistryTypeMCPB:
		return registries.ValidateMCPB(ctx, rp, serverName)
	default:
		return fmt.Errorf("unsupported registry type: %s", pkg.RegistryType)
	}
}
