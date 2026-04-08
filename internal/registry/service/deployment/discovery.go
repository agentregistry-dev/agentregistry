package deployment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"strings"

	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

func shouldIncludeDiscoveredDeployments(filter *models.DeploymentFilter) bool {
	if filter == nil || filter.Origin == nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(*filter.Origin), originDiscovered)
}

// DiscoveredDeploymentID returns a stable ID for a deployment discovered from a platform
// that has not been previously stored in the registry.
func DiscoveredDeploymentID(providerID, resourceType, name, version string) string {
	return DiscoveredDeploymentIDWithNamespace(providerID, resourceType, name, version, "")
}

// DiscoveredDeploymentIDWithNamespace is like DiscoveredDeploymentID but includes a
// namespace component (e.g. Kubernetes namespace) for platforms that require it.
func DiscoveredDeploymentIDWithNamespace(providerID, resourceType, name, version, namespace string) string {
	raw := strings.ToLower(strings.TrimSpace(providerID)) + "|" +
		strings.ToLower(strings.TrimSpace(resourceType)) + "|" +
		strings.TrimSpace(name) + "|" +
		strings.TrimSpace(version) + "|" +
		strings.TrimSpace(namespace)
	sum := sha256.Sum256([]byte(raw))
	return "discovered-" + hex.EncodeToString(sum[:16])
}

func discoveredDeploymentNamespace(dep *models.Deployment) string {
	if dep == nil {
		return ""
	}
	meta := models.KubernetesProviderMetadata{}
	if err := dep.ProviderMetadata.UnmarshalInto(&meta); err == nil {
		if namespace := strings.TrimSpace(meta.Namespace); namespace != "" {
			return namespace
		}
	}
	return ""
}

func matchesDiscoveredDeploymentFilter(filter *models.DeploymentFilter, dep *models.Deployment, provider *models.Provider) bool {
	if filter == nil {
		return true
	}
	if filter.ProviderID != nil && strings.TrimSpace(dep.ProviderID) != strings.TrimSpace(*filter.ProviderID) {
		return false
	}
	if filter.Platform != nil && provider != nil && !strings.EqualFold(strings.TrimSpace(provider.Platform), strings.TrimSpace(*filter.Platform)) {
		return false
	}
	if filter.ResourceType != nil && dep.ResourceType != *filter.ResourceType {
		return false
	}
	if filter.Status != nil && dep.Status != *filter.Status {
		return false
	}
	if filter.Origin != nil && !strings.EqualFold(strings.TrimSpace(dep.Origin), strings.TrimSpace(*filter.Origin)) {
		return false
	}
	if filter.ResourceName != nil && !strings.Contains(strings.ToLower(dep.ServerName), strings.ToLower(*filter.ResourceName)) {
		return false
	}
	return true
}

func (s *registry) appendDiscoveredDeployments(ctx context.Context, deployments []*models.Deployment, filter *models.DeploymentFilter) []*models.Deployment {
	var platformFilter *string
	if filter != nil {
		platformFilter = filter.Platform
	}
	platform := ""
	if platformFilter != nil {
		platform = *platformFilter
	}

	seenDeploymentIDs := make(map[string]struct{}, len(deployments))
	for _, dep := range deployments {
		if dep == nil {
			continue
		}
		if id := strings.TrimSpace(dep.ID); id != "" {
			seenDeploymentIDs[id] = struct{}{}
		}
	}

	providers, err := s.providers.ListProviders(ctx, platform)
	if err != nil {
		log.Printf("Warning: Failed to list providers for discovery: %v", err)
		return deployments
	}

	for _, provider := range providers {
		if provider == nil {
			continue
		}
		if filter != nil && filter.ProviderID != nil && strings.TrimSpace(*filter.ProviderID) != "" &&
			!strings.EqualFold(strings.TrimSpace(provider.ID), strings.TrimSpace(*filter.ProviderID)) {
			continue
		}

		adapter, err := s.ResolveDeploymentAdapter(provider.Platform)
		if err != nil {
			log.Printf("Warning: Failed to resolve deployment adapter for provider %s (%s): %v", provider.ID, provider.Platform, err)
			continue
		}
		discovered, err := adapter.Discover(ctx, provider.ID)
		if err != nil {
			log.Printf("Warning: Failed to discover deployments for provider %s: %v", provider.ID, err)
			continue
		}

		for _, dep := range discovered {
			if dep == nil {
				continue
			}
			if strings.TrimSpace(dep.ProviderID) == "" {
				dep.ProviderID = provider.ID
			}
			if strings.TrimSpace(dep.Origin) == "" {
				dep.Origin = originDiscovered
			}
			if strings.TrimSpace(dep.ID) == "" {
				dep.ID = DiscoveredDeploymentIDWithNamespace(
					dep.ProviderID,
					dep.ResourceType,
					dep.ServerName,
					dep.Version,
					discoveredDeploymentNamespace(dep),
				)
			}
			if _, seen := seenDeploymentIDs[dep.ID]; seen {
				continue
			}
			if !matchesDiscoveredDeploymentFilter(filter, dep, provider) {
				continue
			}
			seenDeploymentIDs[dep.ID] = struct{}{}
			deployments = append(deployments, dep)
		}
	}

	return deployments
}

func (s *registry) getDiscoveredDeploymentByID(ctx context.Context, id string) (*models.Deployment, error) {
	discoveredID := strings.TrimSpace(id)
	if !strings.HasPrefix(discoveredID, "discovered-") {
		return nil, database.ErrNotFound
	}

	origin := originDiscovered
	deployments, err := s.ListDeployments(ctx, &models.DeploymentFilter{Origin: &origin})
	if err != nil {
		return nil, err
	}
	for _, dep := range deployments {
		if dep != nil && dep.ID == discoveredID {
			return dep, nil
		}
	}
	return nil, database.ErrNotFound
}
