package database

import (
	"context"
	"errors"
	"time"

	agentmodels "github.com/agentregistry-dev/agentregistry/internal/models"
	skillmodels "github.com/agentregistry-dev/agentregistry/internal/models"
	"github.com/jackc/pgx/v5"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// Common database errors
var (
	ErrNotFound          = errors.New("record not found")
	ErrAlreadyExists     = errors.New("record already exists")
	ErrInvalidInput      = errors.New("invalid input")
	ErrDatabase          = errors.New("database error")
	ErrInvalidVersion    = errors.New("invalid version: cannot publish duplicate version")
	ErrMaxServersReached = errors.New("maximum number of versions for this server reached (10000): please reach out at https://github.com/modelcontextprotocol/registry to explain your use case")
)

// ServerFilter defines filtering options for server queries
type ServerFilter struct {
	Name          *string    // for finding versions of same server
	RemoteURL     *string    // for duplicate URL detection
	UpdatedSince  *time.Time // for incremental sync filtering
	SubstringName *string    // for substring search on name
	Version       *string    // for exact version matching
	IsLatest      *bool      // for filtering latest versions only
}

// SkillFilter defines filtering options for skill queries (mirrors ServerFilter)
type SkillFilter struct {
	Name          *string    // for finding versions of same skill
	RemoteURL     *string    // for duplicate URL detection
	UpdatedSince  *time.Time // for incremental sync filtering
	SubstringName *string    // for substring search on name
	Version       *string    // for exact version matching
	IsLatest      *bool      // for filtering latest versions only
}

// AgentFilter defines filtering options for agent queries (mirrors ServerFilter)
type AgentFilter struct {
	Name          *string    // for finding versions of same agent
	RemoteURL     *string    // for duplicate URL detection
	UpdatedSince  *time.Time // for incremental sync filtering
	SubstringName *string    // for substring search on name
	Version       *string    // for exact version matching
	IsLatest      *bool      // for filtering latest versions only
}

// Database defines the interface for database operations
type Database interface {
	// CreateServer inserts a new server version with official metadata
	CreateServer(ctx context.Context, tx pgx.Tx, serverJSON *apiv0.ServerJSON, officialMeta *apiv0.RegistryExtensions) (*apiv0.ServerResponse, error)
	// UpdateServer updates an existing server record
	UpdateServer(ctx context.Context, tx pgx.Tx, serverName, version string, serverJSON *apiv0.ServerJSON) (*apiv0.ServerResponse, error)
	// SetServerStatus updates the status of a specific server version
	SetServerStatus(ctx context.Context, tx pgx.Tx, serverName, version string, status string) (*apiv0.ServerResponse, error)
	// ListServers retrieve server entries with optional filtering
	ListServers(ctx context.Context, tx pgx.Tx, filter *ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error)
	// GetServerByName retrieve a single server by its name
	GetServerByName(ctx context.Context, tx pgx.Tx, serverName string) (*apiv0.ServerResponse, error)
	// GetServerByNameAndVersion retrieve specific version of a server by server name and version
	GetServerByNameAndVersion(ctx context.Context, tx pgx.Tx, serverName string, version string) (*apiv0.ServerResponse, error)
	// GetAllVersionsByServerName retrieve all versions of a server by server name
	GetAllVersionsByServerName(ctx context.Context, tx pgx.Tx, serverName string) ([]*apiv0.ServerResponse, error)
	// GetCurrentLatestVersion retrieve the current latest version of a server by server name
	GetCurrentLatestVersion(ctx context.Context, tx pgx.Tx, serverName string) (*apiv0.ServerResponse, error)
	// CountServerVersions count the number of versions for a server
	CountServerVersions(ctx context.Context, tx pgx.Tx, serverName string) (int, error)
	// CheckVersionExists check if a specific version exists for a server
	CheckVersionExists(ctx context.Context, tx pgx.Tx, serverName, version string) (bool, error)
	// UnmarkAsLatest marks the current latest version of a server as no longer latest
	UnmarkAsLatest(ctx context.Context, tx pgx.Tx, serverName string) error
	// AcquirePublishLock acquires an exclusive advisory lock for publishing a server
	// This prevents race conditions when multiple versions are published concurrently
	AcquirePublishLock(ctx context.Context, tx pgx.Tx, serverName string) error
	// InTransaction executes a function within a database transaction
	InTransaction(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error
	// Close closes the database connection
	Close() error

	// Agents API
	// CreateAgent inserts a new agent version with official metadata
	CreateAgent(ctx context.Context, tx pgx.Tx, agentJSON *agentmodels.AgentJSON, officialMeta *agentmodels.AgentRegistryExtensions) (*agentmodels.AgentResponse, error)
	// UpdateAgent updates an existing agent record
	UpdateAgent(ctx context.Context, tx pgx.Tx, agentName, version string, agentJSON *agentmodels.AgentJSON) (*agentmodels.AgentResponse, error)
	// SetAgentStatus updates the status of a specific agent version
	SetAgentStatus(ctx context.Context, tx pgx.Tx, agentName, version string, status string) (*agentmodels.AgentResponse, error)
	// ListAgents retrieve agent entries with optional filtering
	ListAgents(ctx context.Context, tx pgx.Tx, filter *AgentFilter, cursor string, limit int) ([]*agentmodels.AgentResponse, string, error)
	// GetAgentByName retrieve a single agent by its name (latest)
	GetAgentByName(ctx context.Context, tx pgx.Tx, agentName string) (*agentmodels.AgentResponse, error)
	// GetAgentByNameAndVersion retrieve specific version of an agent by name and version
	GetAgentByNameAndVersion(ctx context.Context, tx pgx.Tx, agentName string, version string) (*agentmodels.AgentResponse, error)
	// GetAllVersionsByAgentName retrieve all versions of an agent
	GetAllVersionsByAgentName(ctx context.Context, tx pgx.Tx, agentName string) ([]*agentmodels.AgentResponse, error)
	// GetCurrentLatestAgentVersion retrieve current latest version of an agent
	GetCurrentLatestAgentVersion(ctx context.Context, tx pgx.Tx, agentName string) (*agentmodels.AgentResponse, error)
	// CountAgentVersions count the number of versions for an agent
	CountAgentVersions(ctx context.Context, tx pgx.Tx, agentName string) (int, error)
	// CheckAgentVersionExists check if a specific version exists for an agent
	CheckAgentVersionExists(ctx context.Context, tx pgx.Tx, agentName, version string) (bool, error)
	// UnmarkAgentAsLatest marks the current latest version of an agent as no longer latest
	UnmarkAgentAsLatest(ctx context.Context, tx pgx.Tx, agentName string) error

	// Skills API
	// CreateSkill inserts a new skill version with official metadata
	CreateSkill(ctx context.Context, tx pgx.Tx, skillJSON *skillmodels.SkillJSON, officialMeta *skillmodels.SkillRegistryExtensions) (*skillmodels.SkillResponse, error)
	// UpdateSkill updates an existing skill record
	UpdateSkill(ctx context.Context, tx pgx.Tx, skillName, version string, skillJSON *skillmodels.SkillJSON) (*skillmodels.SkillResponse, error)
	// SetSkillStatus updates the status of a specific skill version
	SetSkillStatus(ctx context.Context, tx pgx.Tx, skillName, version string, status string) (*skillmodels.SkillResponse, error)
	// ListSkills retrieve skill entries with optional filtering
	ListSkills(ctx context.Context, tx pgx.Tx, filter *SkillFilter, cursor string, limit int) ([]*skillmodels.SkillResponse, string, error)
	// GetSkillByName retrieve a single skill by its name (latest)
	GetSkillByName(ctx context.Context, tx pgx.Tx, skillName string) (*skillmodels.SkillResponse, error)
	// GetSkillByNameAndVersion retrieve specific version of a skill by name and version
	GetSkillByNameAndVersion(ctx context.Context, tx pgx.Tx, skillName string, version string) (*skillmodels.SkillResponse, error)
	// GetAllVersionsBySkillName retrieve all versions of a skill
	GetAllVersionsBySkillName(ctx context.Context, tx pgx.Tx, skillName string) ([]*skillmodels.SkillResponse, error)
	// GetCurrentLatestSkillVersion retrieve current latest version of a skill
	GetCurrentLatestSkillVersion(ctx context.Context, tx pgx.Tx, skillName string) (*skillmodels.SkillResponse, error)
	// CountSkillVersions count the number of versions for a skill
	CountSkillVersions(ctx context.Context, tx pgx.Tx, skillName string) (int, error)
	// CheckSkillVersionExists check if a specific version exists for a skill
	CheckSkillVersionExists(ctx context.Context, tx pgx.Tx, skillName, version string) (bool, error)
	// UnmarkSkillAsLatest marks the current latest version of a skill as no longer latest
	UnmarkSkillAsLatest(ctx context.Context, tx pgx.Tx, skillName string) error
}

// InTransactionT is a generic helper that wraps InTransaction for functions returning a value
// This exists because Go does not support generic methods on interfaces - only the Database interface
// method InTransaction (without generics) can exist, so we provide this generic wrapper function.
// This is a common pattern in Go for working around this language limitation.
func InTransactionT[T any](ctx context.Context, db Database, fn func(ctx context.Context, tx pgx.Tx) (T, error)) (T, error) {
	var result T
	var fnErr error

	err := db.InTransaction(ctx, func(txCtx context.Context, tx pgx.Tx) error {
		result, fnErr = fn(txCtx, tx)
		return fnErr
	})

	if err != nil {
		var zero T
		return zero, err
	}

	return result, nil
}
