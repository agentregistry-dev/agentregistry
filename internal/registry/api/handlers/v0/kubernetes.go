package v0

import (
	"context"
	"net/http"

	"github.com/agentregistry-dev/agentregistry/internal/models"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/danielgtaylor/huma/v2"
)

// ListKubernetesDeploymentsInput represents query parameters for listing Kubernetes resources
type ListKubernetesDeploymentsInput struct {
	Namespace string `query:"namespace" json:"namespace,omitempty" doc:"Filter by namespace (empty for all namespaces)" required:"false"`
}

// ListKubernetesDeploymentsResponse represents the response for listing Kubernetes resources
type ListKubernetesDeploymentsResponse struct {
	Body struct {
		Resources []models.KubernetesResource `json:"resources" doc:"List of Kubernetes resources"`
		Count     int                         `json:"count" doc:"Total number of resources"`
	}
}

// RegisterKubernetesEndpoints registers all Kubernetes resource discovery endpoints
func RegisterKubernetesEndpoints(api huma.API, basePath string, registry service.RegistryService) {
	// List Kubernetes resources (agents, MCP servers)
	huma.Register(api, huma.Operation{
		OperationID: "list-kubernetes-resources",
		Method:      http.MethodGet,
		Path:        basePath + "/kubernetes/resources",
		Summary:     "List Kubernetes resources",
		Description: "Retrieve all agents and MCP servers discovered in Kubernetes. External resources (not managed by the registry) are marked with isExternal: true.",
		Tags:        []string{"kubernetes"},
	}, func(ctx context.Context, input *ListKubernetesDeploymentsInput) (*ListKubernetesDeploymentsResponse, error) {
		resources, err := registry.ListKubernetesDeployments(ctx, input.Namespace)
		if err != nil {
			return nil, huma.Error500InternalServerError("Failed to list Kubernetes resources", err)
		}

		resp := &ListKubernetesDeploymentsResponse{}
		resp.Body.Resources = resources
		resp.Body.Count = len(resources)

		return resp, nil
	})
}
