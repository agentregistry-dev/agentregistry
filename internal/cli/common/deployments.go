package common

import (
	"fmt"

	"github.com/agentregistry-dev/agentregistry/internal/client"
)

// BuildDeploymentCounts indexes deployment rows by resource name and version.
func BuildDeploymentCounts(deployments []*client.DeploymentResponse, resourceType string) map[string]map[string]int {
	counts := make(map[string]map[string]int)
	for _, deployment := range deployments {
		if deployment == nil || deployment.ResourceType != resourceType {
			continue
		}
		if counts[deployment.ServerName] == nil {
			counts[deployment.ServerName] = make(map[string]int)
		}
		counts[deployment.ServerName][deployment.Version]++
	}
	return counts
}

// DeployedStatus returns a display status for name/version deployment counts.
func DeployedStatus(counts map[string]map[string]int, name, version string, includeOtherVersionsMessage bool) string {
	resourceDeployments := counts[name]
	if resourceDeployments == nil {
		return "False"
	}

	totalForVersion := resourceDeployments[version]
	switch {
	case totalForVersion > 1:
		return fmt.Sprintf("True (%d)", totalForVersion)
	case totalForVersion == 1:
		return "True"
	case includeOtherVersionsMessage && hasAnyDeployment(resourceDeployments):
		return "False (other versions deployed)"
	default:
		return "False"
	}
}

func hasAnyDeployment(versions map[string]int) bool {
	for _, count := range versions {
		if count > 0 {
			return true
		}
	}
	return false
}
