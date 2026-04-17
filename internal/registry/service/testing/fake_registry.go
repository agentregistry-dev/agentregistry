// Package testing provides test fakes for the registry service layer.
// It is intended for use in integration tests that need an in-process HTTP server
// without a real database.
package testing

import (
	"context"
	"errors"

	platformtypes "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/types"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	registrytypes "github.com/agentregistry-dev/agentregistry/pkg/types"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// ErrNotFound is returned by fake methods when no hook is set and nothing is found.
var ErrNotFound = errors.New("not found")

// FakeRegistry is a test double that implements all per-domain service Registry
// interfaces. Each domain operation is controlled by a replaceable function field.
// Unset fields return sensible zero-value responses rather than panicking, so tests
// only need to configure the hooks they care about.
type FakeRegistry struct {
	// Agent hooks
	CreateAgentFn              func(ctx context.Context, req *models.AgentJSON) (*models.AgentResponse, error)
	ApplyAgentFn               func(ctx context.Context, req *models.AgentJSON) (*models.AgentResponse, error)
	DeleteAgentFn              func(ctx context.Context, name, version string) error
	ListAgentsFn               func(ctx context.Context, filter *database.AgentFilter, cursor string, limit int) ([]*models.AgentResponse, string, error)
	GetAgentByNameFn           func(ctx context.Context, name string) (*models.AgentResponse, error)
	GetAgentByNameAndVersionFn func(ctx context.Context, name, version string) (*models.AgentResponse, error)

	// Server (MCP) hooks
	CreateServerFn func(ctx context.Context, req *apiv0.ServerJSON) (*apiv0.ServerResponse, error)
	ApplyServerFn  func(ctx context.Context, req *apiv0.ServerJSON) (*apiv0.ServerResponse, error)
	DeleteServerFn func(ctx context.Context, name, version string) error
	ListServersFn  func(ctx context.Context, filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error)

	// Skill hooks
	ApplySkillFn func(ctx context.Context, req *models.SkillJSON) (*models.SkillResponse, error)

	// Prompt hooks
	ApplyPromptFn func(ctx context.Context, req *models.PromptJSON) (*models.PromptResponse, error)
}

// NewFakeRegistry creates a FakeRegistry with all hooks unset (nil).
// Calling an unset hook returns a zero-value response without error unless
// the method contract requires a non-nil return, in which case it returns ErrNotFound.
func NewFakeRegistry() *FakeRegistry {
	return &FakeRegistry{}
}

// -- agentsvc.Registry implementation --

func (f *FakeRegistry) ListAgents(ctx context.Context, filter *database.AgentFilter, cursor string, limit int) ([]*models.AgentResponse, string, error) {
	if f.ListAgentsFn != nil {
		return f.ListAgentsFn(ctx, filter, cursor, limit)
	}
	return nil, "", nil
}

func (f *FakeRegistry) GetAgent(ctx context.Context, name string) (*models.AgentResponse, error) {
	if f.GetAgentByNameFn != nil {
		return f.GetAgentByNameFn(ctx, name)
	}
	return nil, ErrNotFound
}

func (f *FakeRegistry) GetAgentVersion(ctx context.Context, name, version string) (*models.AgentResponse, error) {
	if f.GetAgentByNameAndVersionFn != nil {
		return f.GetAgentByNameAndVersionFn(ctx, name, version)
	}
	return nil, ErrNotFound
}

func (f *FakeRegistry) GetAgentVersions(_ context.Context, _ string) ([]*models.AgentResponse, error) {
	return nil, nil
}

func (f *FakeRegistry) GetAgentEmbeddingMetadata(_ context.Context, _, _ string) (*database.SemanticEmbeddingMetadata, error) {
	return nil, nil
}

func (f *FakeRegistry) PublishAgent(ctx context.Context, req *models.AgentJSON) (*models.AgentResponse, error) {
	if f.CreateAgentFn != nil {
		return f.CreateAgentFn(ctx, req)
	}
	return &models.AgentResponse{Agent: *req}, nil
}

func (f *FakeRegistry) ApplyAgent(ctx context.Context, req *models.AgentJSON) (*models.AgentResponse, error) {
	if f.ApplyAgentFn != nil {
		return f.ApplyAgentFn(ctx, req)
	}
	// Fall back to CreateAgentFn for backward compatibility with tests that use it.
	if f.CreateAgentFn != nil {
		return f.CreateAgentFn(ctx, req)
	}
	return &models.AgentResponse{Agent: *req}, nil
}

func (f *FakeRegistry) DeleteAgent(ctx context.Context, name, version string) error {
	if f.DeleteAgentFn != nil {
		return f.DeleteAgentFn(ctx, name, version)
	}
	return nil
}

func (f *FakeRegistry) SetAgentEmbedding(_ context.Context, _, _ string, _ *database.SemanticEmbedding) error {
	return nil
}

func (f *FakeRegistry) ResolveAgentManifestSkills(_ context.Context, _ *models.AgentManifest) ([]platformtypes.AgentSkillRef, error) {
	return nil, nil
}

func (f *FakeRegistry) ResolveAgentManifestPrompts(_ context.Context, _ *models.AgentManifest) ([]platformtypes.ResolvedPrompt, error) {
	return nil, nil
}

// -- serversvc.Registry implementation --

func (f *FakeRegistry) ListServers(ctx context.Context, filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error) {
	if f.ListServersFn != nil {
		return f.ListServersFn(ctx, filter, cursor, limit)
	}
	return nil, "", nil
}

func (f *FakeRegistry) GetServer(_ context.Context, _ string) (*apiv0.ServerResponse, error) {
	return nil, ErrNotFound
}

func (f *FakeRegistry) GetServerVersion(_ context.Context, _, _ string) (*apiv0.ServerResponse, error) {
	return nil, ErrNotFound
}

func (f *FakeRegistry) GetServerVersions(_ context.Context, _ string) ([]*apiv0.ServerResponse, error) {
	return nil, nil
}

func (f *FakeRegistry) GetServerReadme(_ context.Context, _, _ string) (*database.ServerReadme, error) {
	return nil, ErrNotFound
}

func (f *FakeRegistry) GetLatestServerReadme(_ context.Context, _ string) (*database.ServerReadme, error) {
	return nil, ErrNotFound
}

func (f *FakeRegistry) GetServerEmbeddingMetadata(_ context.Context, _, _ string) (*database.SemanticEmbeddingMetadata, error) {
	return nil, nil
}

func (f *FakeRegistry) PublishServer(ctx context.Context, req *apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
	if f.CreateServerFn != nil {
		return f.CreateServerFn(ctx, req)
	}
	return &apiv0.ServerResponse{Server: *req}, nil
}

func (f *FakeRegistry) ApplyServer(ctx context.Context, req *apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
	if f.ApplyServerFn != nil {
		return f.ApplyServerFn(ctx, req)
	}
	if f.CreateServerFn != nil {
		return f.CreateServerFn(ctx, req)
	}
	return &apiv0.ServerResponse{Server: *req}, nil
}

func (f *FakeRegistry) UpdateServer(_ context.Context, _, _ string, req *apiv0.ServerJSON, _ *string) (*apiv0.ServerResponse, error) {
	return &apiv0.ServerResponse{Server: *req}, nil
}

func (f *FakeRegistry) SetServerReadme(_ context.Context, _, _ string, _ []byte, _ string) error {
	return nil
}

func (f *FakeRegistry) DeleteServer(ctx context.Context, name, version string) error {
	if f.DeleteServerFn != nil {
		return f.DeleteServerFn(ctx, name, version)
	}
	return nil
}

func (f *FakeRegistry) SetServerEmbedding(_ context.Context, _, _ string, _ *database.SemanticEmbedding) error {
	return nil
}

// -- skillsvc.Registry implementation --

func (f *FakeRegistry) ListSkills(_ context.Context, _ *database.SkillFilter, _ string, _ int) ([]*models.SkillResponse, string, error) {
	return nil, "", nil
}

func (f *FakeRegistry) GetSkill(_ context.Context, _ string) (*models.SkillResponse, error) {
	return nil, ErrNotFound
}

func (f *FakeRegistry) GetSkillVersion(_ context.Context, _, _ string) (*models.SkillResponse, error) {
	return nil, ErrNotFound
}

func (f *FakeRegistry) GetSkillVersions(_ context.Context, _ string) ([]*models.SkillResponse, error) {
	return nil, nil
}

func (f *FakeRegistry) PublishSkill(_ context.Context, req *models.SkillJSON) (*models.SkillResponse, error) {
	return &models.SkillResponse{Skill: *req}, nil
}

func (f *FakeRegistry) ApplySkill(ctx context.Context, req *models.SkillJSON) (*models.SkillResponse, error) {
	if f.ApplySkillFn != nil {
		return f.ApplySkillFn(ctx, req)
	}
	return &models.SkillResponse{Skill: *req}, nil
}

func (f *FakeRegistry) DeleteSkill(_ context.Context, _, _ string) error {
	return nil
}

// -- promptsvc.Registry implementation --

func (f *FakeRegistry) ListPrompts(_ context.Context, _ *database.PromptFilter, _ string, _ int) ([]*models.PromptResponse, string, error) {
	return nil, "", nil
}

func (f *FakeRegistry) GetPrompt(_ context.Context, _ string) (*models.PromptResponse, error) {
	return nil, ErrNotFound
}

func (f *FakeRegistry) GetPromptVersion(_ context.Context, _, _ string) (*models.PromptResponse, error) {
	return nil, ErrNotFound
}

func (f *FakeRegistry) GetPromptVersions(_ context.Context, _ string) ([]*models.PromptResponse, error) {
	return nil, nil
}

func (f *FakeRegistry) PublishPrompt(_ context.Context, req *models.PromptJSON) (*models.PromptResponse, error) {
	return &models.PromptResponse{Prompt: *req}, nil
}

func (f *FakeRegistry) ApplyPrompt(ctx context.Context, req *models.PromptJSON) (*models.PromptResponse, error) {
	if f.ApplyPromptFn != nil {
		return f.ApplyPromptFn(ctx, req)
	}
	return &models.PromptResponse{Prompt: *req}, nil
}

func (f *FakeRegistry) DeletePrompt(_ context.Context, _, _ string) error {
	return nil
}

// -- providersvc.Registry implementation --

func (f *FakeRegistry) ListProviders(_ context.Context, _ string) ([]*models.Provider, error) {
	return nil, nil
}

func (f *FakeRegistry) RegisterProvider(_ context.Context, _ *models.CreateProviderInput) (*models.Provider, error) {
	return nil, ErrNotFound
}

func (f *FakeRegistry) GetProvider(_ context.Context, _ string) (*models.Provider, error) {
	return nil, ErrNotFound
}

func (f *FakeRegistry) ResolveProvider(_ context.Context, _, _ string) (*models.Provider, error) {
	return nil, ErrNotFound
}

func (f *FakeRegistry) UpdateProvider(_ context.Context, _, _ string, _ *models.UpdateProviderInput) (*models.Provider, error) {
	return nil, ErrNotFound
}

func (f *FakeRegistry) DeleteProvider(_ context.Context, _, _ string) error {
	return nil
}

func (f *FakeRegistry) ApplyProvider(_ context.Context, _, _ string, _ *models.UpdateProviderInput) (*models.Provider, error) {
	return nil, ErrNotFound
}

func (f *FakeRegistry) PlatformAdapters() map[string]registrytypes.ProviderPlatformAdapter {
	return nil
}

// -- deploymentsvc.Registry implementation --

func (f *FakeRegistry) ListDeployments(_ context.Context, _ *models.DeploymentFilter) ([]*models.Deployment, error) {
	return nil, nil
}

func (f *FakeRegistry) GetDeployment(_ context.Context, _ string) (*models.Deployment, error) {
	return nil, ErrNotFound
}

func (f *FakeRegistry) DeployServer(_ context.Context, _, _ string, _ map[string]string, _ bool, _ string) (*models.Deployment, error) {
	return nil, ErrNotFound
}

func (f *FakeRegistry) DeployAgent(_ context.Context, _, _ string, _ map[string]string, _ bool, _ string) (*models.Deployment, error) {
	return nil, ErrNotFound
}

func (f *FakeRegistry) DeleteDeployment(_ context.Context, _ string) error {
	return nil
}

func (f *FakeRegistry) LaunchDeployment(_ context.Context, _ *models.Deployment) (*models.Deployment, error) {
	return nil, ErrNotFound
}

func (f *FakeRegistry) UndeployDeployment(_ context.Context, _ *models.Deployment) error {
	return nil
}

func (f *FakeRegistry) GetDeploymentLogs(_ context.Context, _ *models.Deployment) ([]string, error) {
	return nil, nil
}

func (f *FakeRegistry) CancelDeployment(_ context.Context, _ *models.Deployment) error {
	return nil
}

func (f *FakeRegistry) ApplyAgentDeployment(_ context.Context, _, _, _ string, _ map[string]string, _ models.JSONObject, _, _ bool) (*models.Deployment, error) {
	return nil, ErrNotFound
}

func (f *FakeRegistry) ApplyServerDeployment(_ context.Context, _, _, _ string, _ map[string]string, _ models.JSONObject, _, _ bool) (*models.Deployment, error) {
	return nil, ErrNotFound
}
