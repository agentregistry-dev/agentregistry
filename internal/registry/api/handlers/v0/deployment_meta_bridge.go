package v0

import (
	"context"
	"slices"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
)

type deploymentResourceKey struct {
	resourceType string
	resourceName string
}

func deploymentResourceIndex(ctx context.Context, registry service.RegistryService) map[deploymentResourceKey][]models.DeploymentSummary {
	deployments, err := registry.GetDeployments(ctx, nil)
	if err != nil {
		return map[deploymentResourceKey][]models.DeploymentSummary{}
	}

	index := make(map[deploymentResourceKey][]models.DeploymentSummary)
	for _, deployment := range deployments {
		if deployment == nil {
			continue
		}

		resourceType := strings.ToLower(strings.TrimSpace(deployment.ResourceType))
		resourceName := strings.TrimSpace(deployment.ServerName)
		if resourceType == "" || resourceName == "" {
			continue
		}

		key := deploymentResourceKey{
			resourceType: resourceType,
			resourceName: resourceName,
		}
		index[key] = append(index[key], models.DeploymentSummary{
			ID:         deployment.ID,
			ProviderID: deployment.ProviderID,
			Status:     deployment.Status,
			Origin:     deployment.Origin,
			Version:    deployment.Version,
			DeployedAt: deployment.DeployedAt,
			UpdatedAt:  deployment.UpdatedAt,
		})
	}

	for key := range index {
		slices.SortFunc(index[key], func(a, b models.DeploymentSummary) int {
			switch {
			case a.UpdatedAt.After(b.UpdatedAt):
				return -1
			case a.UpdatedAt.Before(b.UpdatedAt):
				return 1
			default:
				return 0
			}
		})
	}

	return index
}

func deploymentAppliesToVersion(summary models.DeploymentSummary, itemVersion string, itemIsLatest bool) bool {
	deploymentVersion := strings.TrimSpace(summary.Version)
	if deploymentVersion == "" {
		return true
	}
	if strings.EqualFold(deploymentVersion, itemVersion) {
		return true
	}
	return strings.EqualFold(deploymentVersion, "latest") && itemIsLatest
}

func attachServerDeploymentMeta(
	servers []models.ServerResponse,
	deploymentIndex map[deploymentResourceKey][]models.DeploymentSummary,
) []models.ServerResponse {
	out := make([]models.ServerResponse, len(servers))
	copy(out, servers)

	for i := range out {
		serverName := strings.TrimSpace(out[i].Server.Name)
		if serverName == "" {
			continue
		}
		key := deploymentResourceKey{
			resourceType: "mcp",
			resourceName: serverName,
		}
		summaries := deploymentIndex[key]
		filtered := make([]models.DeploymentSummary, 0, len(summaries))
		isLatest := out[i].Meta.Official != nil && out[i].Meta.Official.IsLatest
		for _, summary := range summaries {
			if deploymentAppliesToVersion(summary, out[i].Server.Version, isLatest) {
				filtered = append(filtered, summary)
			}
		}
		if len(filtered) > 0 {
			out[i].Meta.Deployments = &models.ResourceDeploymentsMeta{
				Deployments: filtered,
				Count:       len(filtered),
			}
		}
	}

	return out
}

func attachAgentDeploymentMeta(
	agents []models.AgentResponse,
	deploymentIndex map[deploymentResourceKey][]models.DeploymentSummary,
) []models.AgentResponse {
	out := make([]models.AgentResponse, len(agents))
	copy(out, agents)

	for i := range out {
		agentName := strings.TrimSpace(out[i].Agent.Name)
		if agentName == "" {
			continue
		}
		key := deploymentResourceKey{
			resourceType: "agent",
			resourceName: agentName,
		}
		summaries := deploymentIndex[key]
		filtered := make([]models.DeploymentSummary, 0, len(summaries))
		isLatest := out[i].Meta.Official != nil && out[i].Meta.Official.IsLatest
		for _, summary := range summaries {
			if deploymentAppliesToVersion(summary, out[i].Agent.Version, isLatest) {
				filtered = append(filtered, summary)
			}
		}
		if len(filtered) > 0 {
			out[i].Meta.Deployments = &models.ResourceDeploymentsMeta{
				Deployments: filtered,
				Count:       len(filtered),
			}
		}
	}

	return out
}
