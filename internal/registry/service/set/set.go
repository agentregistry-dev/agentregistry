package set

import (
	"log/slog"

	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	"github.com/agentregistry-dev/agentregistry/internal/registry/embeddings"
	agentsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/agent"
	deploymentsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/deployment"
	promptsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/prompt"
	providersvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/provider"
	serversvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/server"
	skillsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/skill"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	registrytypes "github.com/agentregistry-dev/agentregistry/pkg/types"
)

type Dependencies struct {
	StoreDB            database.ServiceDatabase
	ServerStore        database.ServerStore
	AgentStore         database.AgentStore
	SkillStore         database.SkillStore
	PromptStore        database.PromptStore
	ProviderStore      database.ProviderStore
	DeploymentStore    database.DeploymentStore
	Config             *config.Config
	EmbeddingsProvider embeddings.Provider
	DeploymentAdapters map[string]registrytypes.DeploymentPlatformAdapter
	Logger             *slog.Logger
}

type serverDomain struct {
	*serversvc.Service
}

type agentDomain struct {
	*agentsvc.Service
}

type skillDomain struct {
	*skillsvc.Service
}

type promptDomain struct {
	*promptsvc.Service
}

type providerDomain struct {
	*providersvc.Service
}

type deploymentDomain struct {
	*deploymentsvc.Service
}

type Set struct {
	serverDomain
	agentDomain
	skillDomain
	promptDomain
	providerDomain
	deploymentDomain

	server     *serversvc.Service
	agent      *agentsvc.Service
	skill      *skillsvc.Service
	prompt     *promptsvc.Service
	provider   *providersvc.Service
	deployment *deploymentsvc.Service
	config     *config.Config
}

func New(deps Dependencies) *Set {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default().With("component", "registry")
	}

	server := serversvc.New(serversvc.Dependencies{
		StoreDB:            deps.StoreDB,
		Servers:            deps.ServerStore,
		Config:             deps.Config,
		EmbeddingsProvider: deps.EmbeddingsProvider,
		Logger:             logger,
	})
	agent := agentsvc.New(agentsvc.Dependencies{
		StoreDB:            deps.StoreDB,
		Agents:             deps.AgentStore,
		Skills:             deps.SkillStore,
		Prompts:            deps.PromptStore,
		Config:             deps.Config,
		EmbeddingsProvider: deps.EmbeddingsProvider,
		Logger:             logger,
	})
	skill := skillsvc.New(skillsvc.Dependencies{StoreDB: deps.StoreDB, Skills: deps.SkillStore})
	prompt := promptsvc.New(promptsvc.Dependencies{StoreDB: deps.StoreDB, Prompts: deps.PromptStore})
	provider := providersvc.New(providersvc.Dependencies{StoreDB: deps.StoreDB, Providers: deps.ProviderStore})
	deployment := deploymentsvc.New(deploymentsvc.Dependencies{
		StoreDB:            deps.StoreDB,
		Providers:          deps.ProviderStore,
		Servers:            deps.ServerStore,
		Agents:             deps.AgentStore,
		Deployments:        deps.DeploymentStore,
		DeploymentAdapters: deps.DeploymentAdapters,
	})

	return &Set{
		serverDomain:     serverDomain{Service: server},
		agentDomain:      agentDomain{Service: agent},
		skillDomain:      skillDomain{Service: skill},
		promptDomain:     promptDomain{Service: prompt},
		providerDomain:   providerDomain{Service: provider},
		deploymentDomain: deploymentDomain{Service: deployment},
		server:           server,
		agent:            agent,
		skill:            skill,
		prompt:           prompt,
		provider:         provider,
		deployment:       deployment,
		config:           deps.Config,
	}
}

func (s *Set) Config() *config.Config {
	return s.config
}

func (s *Set) Server() *serversvc.Service {
	return s.server
}

func (s *Set) Agent() *agentsvc.Service {
	return s.agent
}

func (s *Set) Skill() *skillsvc.Service {
	return s.skill
}

func (s *Set) Prompt() *promptsvc.Service {
	return s.prompt
}

func (s *Set) Provider() *providersvc.Service {
	return s.provider
}

func (s *Set) Deployment() *deploymentsvc.Service {
	return s.deployment
}
