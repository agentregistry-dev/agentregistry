package router

import "github.com/agentregistry-dev/agentregistry/internal/registry/service"

// APIRouteService defines the registry operations consumed by the HTTP routing layer.
type APIRouteService interface {
	service.ServerService
	service.AgentService
	service.SkillService
	service.PromptService
	service.ProviderService
	service.DeploymentService
}
