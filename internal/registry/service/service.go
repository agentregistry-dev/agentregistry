package service

// RegistryService aggregates the domain-level registry service contracts.
type RegistryService interface {
	ServerService
	AgentService
	SkillService
	PromptService

	ProviderService
	DeploymentService
}
