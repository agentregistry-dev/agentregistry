package service

import (
	deploymentsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/deployment"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service/internal/deployutil"
)

const (
	resourceTypeMCP   = "mcp"
	resourceTypeAgent = "agent"
	originDiscovered  = "discovered"
)

type UnsupportedDeploymentPlatformError = deployutil.UnsupportedDeploymentPlatformError

type DeploymentPlatformStaleCleaner = deployutil.PlatformStaleCleaner

func IsUnsupportedDeploymentPlatformError(err error) bool {
	return deployutil.IsUnsupportedDeploymentPlatformError(err)
}

func discoveredDeploymentID(providerID, resourceType, name, version string) string {
	return deploymentsvc.DiscoveredDeploymentID(providerID, resourceType, name, version)
}

func discoveredDeploymentIDWithNamespace(providerID, resourceType, name, version, namespace string) string {
	return deploymentsvc.DiscoveredDeploymentIDWithNamespace(providerID, resourceType, name, version, namespace)
}
