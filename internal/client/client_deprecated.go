package client

// This file retains typed per-kind client methods (GetServer, CreatePrompt,
// DeleteAgent, etc.) as thin stubs so the imperative CLI packages keep
// compiling during the v1alpha1 refactor. The imperative CLI is being
// replaced by a declarative-only CLI on a separate branch; when that
// branch merges, the callers disappear and this file can be deleted.
//
// Every method returns errDeprecatedImperative at runtime — no server
// call is made. Callers should migrate to the generic
// Get / GetLatest / List / Apply / DeleteViaApply / Delete methods on
// *Client which speak v1alpha1 directly.

import (
	"errors"

	"github.com/agentregistry-dev/agentregistry/pkg/models"
	v0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

var errDeprecatedImperative = errors.New(
	"imperative per-kind client method removed; use declarative Apply/Get/List/Delete on *client.Client (speak v1alpha1 directly)",
)

// --- MCPServer legacy shape ---

func (*Client) GetPublishedServers() ([]*v0.ServerResponse, error) {
	return nil, errDeprecatedImperative
}

func (*Client) GetServer(string) (*v0.ServerResponse, error) {
	return nil, errDeprecatedImperative
}

func (*Client) GetServerVersion(string, string) (*v0.ServerResponse, error) {
	return nil, errDeprecatedImperative
}

func (*Client) GetServerVersions(string) ([]v0.ServerResponse, error) {
	return nil, errDeprecatedImperative
}

func (*Client) CreateMCPServer(*v0.ServerJSON) (*v0.ServerResponse, error) {
	return nil, errDeprecatedImperative
}

func (*Client) DeleteMCPServer(string, string) error {
	return errDeprecatedImperative
}

// --- Skill legacy shape ---

func (*Client) GetSkills() ([]*models.SkillResponse, error) {
	return nil, errDeprecatedImperative
}

func (*Client) GetSkill(string) (*models.SkillResponse, error) {
	return nil, errDeprecatedImperative
}

func (*Client) GetSkillVersion(string, string) (*models.SkillResponse, error) {
	return nil, errDeprecatedImperative
}

func (*Client) GetSkillVersions(string) ([]*models.SkillResponse, error) {
	return nil, errDeprecatedImperative
}

func (*Client) CreateSkill(*models.SkillJSON) (*models.SkillResponse, error) {
	return nil, errDeprecatedImperative
}

func (*Client) DeleteSkill(string, string) error {
	return errDeprecatedImperative
}

// --- Agent legacy shape ---

func (*Client) GetAgents() ([]*models.AgentResponse, error) {
	return nil, errDeprecatedImperative
}

func (*Client) GetAgent(string) (*models.AgentResponse, error) {
	return nil, errDeprecatedImperative
}

func (*Client) GetAgentVersion(string, string) (*models.AgentResponse, error) {
	return nil, errDeprecatedImperative
}

func (*Client) CreateAgent(*models.AgentJSON) (*models.AgentResponse, error) {
	return nil, errDeprecatedImperative
}

func (*Client) DeleteAgent(string, string) error {
	return errDeprecatedImperative
}

// --- Prompt legacy shape ---

func (*Client) GetPrompts() ([]*models.PromptResponse, error) {
	return nil, errDeprecatedImperative
}

func (*Client) GetPrompt(string) (*models.PromptResponse, error) {
	return nil, errDeprecatedImperative
}

func (*Client) GetPromptVersion(string, string) (*models.PromptResponse, error) {
	return nil, errDeprecatedImperative
}

func (*Client) CreatePrompt(*models.PromptJSON) (*models.PromptResponse, error) {
	return nil, errDeprecatedImperative
}

func (*Client) DeletePrompt(string, string) error {
	return errDeprecatedImperative
}

// --- Provider legacy shape ---

func (*Client) GetProviders() ([]*models.Provider, error) {
	return nil, errDeprecatedImperative
}

func (*Client) GetProvider(string) (*models.Provider, error) {
	return nil, errDeprecatedImperative
}

func (*Client) DeleteProvider(string) error {
	return errDeprecatedImperative
}
