package validators

import (
	"context"
	"fmt"

	"github.com/agentregistry-dev/agentregistry/internal/registry/validators/registries"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	v1alpha1registries "github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1/registries"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

// ValidatePackage validates that the package referenced in the server configuration is:
// 1. allowed on the official registry (based on registry base url); and
// 2. owned by the publisher, by checking for a matching server name in the package metadata
//
// OCI validation dispatches to the v1alpha1 port; other registries
// still fall through to the legacy validators until their per-registry
// ports land.
func ValidatePackage(ctx context.Context, pkg model.Package, serverName string) error {
	switch pkg.RegistryType {
	case model.RegistryTypeNPM:
		return registries.ValidateNPM(ctx, pkg, serverName)
	case model.RegistryTypePyPI:
		return registries.ValidatePyPI(ctx, pkg, serverName)
	case model.RegistryTypeNuGet:
		return registries.ValidateNuGet(ctx, pkg, serverName)
	case model.RegistryTypeOCI:
		return v1alpha1registries.ValidateOCI(ctx, v1alpha1.RegistryPackage{
			RegistryType:    pkg.RegistryType,
			Identifier:      pkg.Identifier,
			Version:         pkg.Version,
			RegistryBaseURL: pkg.RegistryBaseURL,
			FileSHA256:      pkg.FileSHA256,
		}, serverName)
	case model.RegistryTypeMCPB:
		return registries.ValidateMCPB(ctx, pkg, serverName)
	default:
		return fmt.Errorf("unsupported registry type: %s", pkg.RegistryType)
	}
}
