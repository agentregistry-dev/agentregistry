package database

import (
	"context"
	"errors"
	"time"

	"github.com/agentregistry-dev/agentregistry/pkg/models"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// Common database errors
var (
	ErrNotFound           = errors.New("record not found")
	ErrForbidden          = errors.New("forbidden")
	ErrAlreadyExists      = errors.New("record already exists")
	ErrInvalidInput       = errors.New("invalid input")
	ErrDatabase           = errors.New("database error")
	ErrInvalidVersion     = errors.New("invalid version: cannot publish duplicate version")
	ErrMaxVersionsReached = errors.New("maximum number of versions reached (10000): please reach out at https://github.com/modelcontextprotocol/registry to explain your use case")
)

// ServerFilter defines filtering options for server queries
type ServerFilter struct {
	Name          *string    // for finding versions of same server
	RemoteURL     *string    // for duplicate URL detection
	UpdatedSince  *time.Time // for incremental sync filtering
	SubstringName *string    // for substring search on name
	Version       *string    // for exact version matching
	IsLatest      *bool      // for filtering latest versions only
	Semantic      *SemanticSearchOptions
}

// ServerReadme represents a stored README blob for a server version
type ServerReadme struct {
	ServerName  string
	Version     string
	Content     []byte
	ContentType string
	SizeBytes   int
	SHA256      []byte
	FetchedAt   time.Time
}

// SkillFilter defines filtering options for skill queries (mirrors ServerFilter)
type SkillFilter struct {
	Name          *string    // for finding versions of same skill
	RemoteURL     *string    // for duplicate URL detection
	UpdatedSince  *time.Time // for incremental sync filtering
	SubstringName *string    // for substring search on name
	Version       *string    // for exact version matching
	IsLatest      *bool      // for filtering latest versions only
	Semantic      *SemanticSearchOptions
}

// AgentFilter defines filtering options for agent queries (mirrors ServerFilter)
type AgentFilter struct {
	Name          *string    // for finding versions of same agent
	RemoteURL     *string    // for duplicate URL detection
	UpdatedSince  *time.Time // for incremental sync filtering
	SubstringName *string    // for substring search on name
	Version       *string    // for exact version matching
	IsLatest      *bool      // for filtering latest versions only
	Semantic      *SemanticSearchOptions
}

// PromptFilter defines filtering options for prompt queries
type PromptFilter struct {
	Name          *string    // for finding versions of same prompt
	UpdatedSince  *time.Time // for incremental sync filtering
	SubstringName *string    // for substring search on name
	Version       *string    // for exact version matching
	IsLatest      *bool      // for filtering latest versions only
}

// SemanticEmbedding captures data stored alongside registry resources for semantic search.
type SemanticEmbedding struct {
	Vector     []float32
	Provider   string
	Model      string
	Dimensions int
	Checksum   string
	Generated  time.Time
}

// SemanticEmbeddingMetadata captures stored metadata about an embedding without the vector payload.
type SemanticEmbeddingMetadata struct {
	HasEmbedding bool
	Provider     string
	Model        string
	Dimensions   int
	Checksum     string
	Generated    time.Time
}

// SemanticSearchOptions drives vector similarity queries when listing resources.
type SemanticSearchOptions struct {
	// RawQuery retains the original search string for embedding generation (service layer use only).
	RawQuery string
	// QueryEmbedding holds the vector representation expected by the database layer.
	QueryEmbedding []float32
	// Threshold filters out matches whose distance exceeds this value (distance metric specific).
	Threshold float64
	// HybridSubstring preserves substring conditions for hybrid search.
	HybridSubstring *string
}

// CommandTag reports metadata about an executed statement.
type CommandTag interface {
	RowsAffected() int64
}

// Rows defines the row-iteration surface repository methods need.
type Rows interface {
	Close()
	Err() error
	Next() bool
	Scan(dest ...any) error
}

// Row defines the single-row scan surface repository methods need.
type Row interface {
	Scan(dest ...any) error
}

type ServerStore interface {
	DeleteServer(ctx context.Context, serverName, version string) error
	CreateServer(ctx context.Context, serverJSON *apiv0.ServerJSON, officialMeta *apiv0.RegistryExtensions) (*apiv0.ServerResponse, error)
	UpdateServer(ctx context.Context, serverName, version string, serverJSON *apiv0.ServerJSON) (*apiv0.ServerResponse, error)
	SetServerStatus(ctx context.Context, serverName, version, status string) (*apiv0.ServerResponse, error)
	ListServers(ctx context.Context, filter *ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error)
	GetServerByName(ctx context.Context, serverName string) (*apiv0.ServerResponse, error)
	GetServerByNameAndVersion(ctx context.Context, serverName, version string) (*apiv0.ServerResponse, error)
	GetAllVersionsByServerName(ctx context.Context, serverName string) ([]*apiv0.ServerResponse, error)
	GetCurrentLatestVersion(ctx context.Context, serverName string) (*apiv0.ServerResponse, error)
	CountServerVersions(ctx context.Context, serverName string) (int, error)
	CheckVersionExists(ctx context.Context, serverName, version string) (bool, error)
	UnmarkAsLatest(ctx context.Context, serverName string) error
	AcquireServerCreateLock(ctx context.Context, serverName string) error
	SetServerEmbedding(ctx context.Context, serverName, version string, embedding *SemanticEmbedding) error
	GetServerEmbeddingMetadata(ctx context.Context, serverName, version string) (*SemanticEmbeddingMetadata, error)
	UpsertServerReadme(ctx context.Context, readme *ServerReadme) error
	GetServerReadme(ctx context.Context, serverName, version string) (*ServerReadme, error)
	GetLatestServerReadme(ctx context.Context, serverName string) (*ServerReadme, error)
}

type ProviderStore interface {
	CreateProvider(ctx context.Context, in *models.CreateProviderInput) (*models.Provider, error)
	ListProviders(ctx context.Context, platform *string) ([]*models.Provider, error)
	GetProviderByID(ctx context.Context, providerID string) (*models.Provider, error)
	UpdateProvider(ctx context.Context, providerID string, in *models.UpdateProviderInput) (*models.Provider, error)
	DeleteProvider(ctx context.Context, providerID string) error
}

type AgentStore interface {
	CreateAgent(ctx context.Context, agentJSON *models.AgentJSON, officialMeta *models.AgentRegistryExtensions) (*models.AgentResponse, error)
	UpdateAgent(ctx context.Context, agentName, version string, agentJSON *models.AgentJSON) (*models.AgentResponse, error)
	SetAgentStatus(ctx context.Context, agentName, version, status string) (*models.AgentResponse, error)
	ListAgents(ctx context.Context, filter *AgentFilter, cursor string, limit int) ([]*models.AgentResponse, string, error)
	GetAgentByName(ctx context.Context, agentName string) (*models.AgentResponse, error)
	GetAgentByNameAndVersion(ctx context.Context, agentName, version string) (*models.AgentResponse, error)
	GetAllVersionsByAgentName(ctx context.Context, agentName string) ([]*models.AgentResponse, error)
	GetCurrentLatestAgentVersion(ctx context.Context, agentName string) (*models.AgentResponse, error)
	CountAgentVersions(ctx context.Context, agentName string) (int, error)
	CheckAgentVersionExists(ctx context.Context, agentName, version string) (bool, error)
	UnmarkAgentAsLatest(ctx context.Context, agentName string) error
	DeleteAgent(ctx context.Context, agentName, version string) error
	SetAgentEmbedding(ctx context.Context, agentName, version string, embedding *SemanticEmbedding) error
	GetAgentEmbeddingMetadata(ctx context.Context, agentName, version string) (*SemanticEmbeddingMetadata, error)
}

type SkillStore interface {
	CreateSkill(ctx context.Context, skillJSON *models.SkillJSON, officialMeta *models.SkillRegistryExtensions) (*models.SkillResponse, error)
	UpdateSkill(ctx context.Context, skillName, version string, skillJSON *models.SkillJSON) (*models.SkillResponse, error)
	SetSkillStatus(ctx context.Context, skillName, version, status string) (*models.SkillResponse, error)
	ListSkills(ctx context.Context, filter *SkillFilter, cursor string, limit int) ([]*models.SkillResponse, string, error)
	GetSkillByName(ctx context.Context, skillName string) (*models.SkillResponse, error)
	GetSkillByNameAndVersion(ctx context.Context, skillName, version string) (*models.SkillResponse, error)
	GetAllVersionsBySkillName(ctx context.Context, skillName string) ([]*models.SkillResponse, error)
	GetCurrentLatestSkillVersion(ctx context.Context, skillName string) (*models.SkillResponse, error)
	CountSkillVersions(ctx context.Context, skillName string) (int, error)
	CheckSkillVersionExists(ctx context.Context, skillName, version string) (bool, error)
	UnmarkSkillAsLatest(ctx context.Context, skillName string) error
	DeleteSkill(ctx context.Context, skillName, version string) error
}

type PromptStore interface {
	CreatePrompt(ctx context.Context, promptJSON *models.PromptJSON, officialMeta *models.PromptRegistryExtensions) (*models.PromptResponse, error)
	ListPrompts(ctx context.Context, filter *PromptFilter, cursor string, limit int) ([]*models.PromptResponse, string, error)
	GetPromptByName(ctx context.Context, promptName string) (*models.PromptResponse, error)
	GetPromptByNameAndVersion(ctx context.Context, promptName, version string) (*models.PromptResponse, error)
	GetAllVersionsByPromptName(ctx context.Context, promptName string) ([]*models.PromptResponse, error)
	GetCurrentLatestPromptVersion(ctx context.Context, promptName string) (*models.PromptResponse, error)
	CountPromptVersions(ctx context.Context, promptName string) (int, error)
	CheckPromptVersionExists(ctx context.Context, promptName, version string) (bool, error)
	UnmarkPromptAsLatest(ctx context.Context, promptName string) error
	DeletePrompt(ctx context.Context, promptName, version string) error
}

type DeploymentStore interface {
	CreateDeployment(ctx context.Context, deployment *models.Deployment) error
	GetDeployments(ctx context.Context, filter *models.DeploymentFilter) ([]*models.Deployment, error)
	GetDeploymentByID(ctx context.Context, id string) (*models.Deployment, error)
	UpdateDeploymentState(ctx context.Context, id string, patch *models.DeploymentStatePatch) error
	RemoveDeploymentByID(ctx context.Context, id string) error
}

// Scope exposes the domain repositories that share the same backing executor.
// Transaction callbacks receive a transaction-bound Scope.
type Scope interface {
	Servers() ServerStore
	Providers() ProviderStore
	Agents() AgentStore
	Skills() SkillStore
	Prompts() PromptStore
	Deployments() DeploymentStore
}

// Transactor provides transaction orchestration without exposing the backing
// transaction object to callers.
type Transactor interface {
	InTransaction(ctx context.Context, fn func(context.Context, Scope) error) error
}

// Store is the root persistence contract used at app composition boundaries.
// Callers read domain stores directly from the root and use InTransaction when
// they need multiple operations to share a transaction.
type Store interface {
	Scope
	Transactor
	Close() error
}

var ErrStoreNotConfigured = errors.New("store is not configured")

func InTransaction(ctx context.Context, tx Transactor, fn func(context.Context, Scope) error) error {
	if tx == nil {
		return ErrStoreNotConfigured
	}
	return tx.InTransaction(ctx, fn)
}

func InTransactionT[T any](ctx context.Context, tx Transactor, fn func(context.Context, Scope) (T, error)) (T, error) {
	var result T
	var fnErr error

	err := InTransaction(ctx, tx, func(txCtx context.Context, scope Scope) error {
		result, fnErr = fn(txCtx, scope)
		return fnErr
	})
	if err != nil {
		var zero T
		return zero, err
	}

	return result, nil
}
