package exporter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/internal/registry/seed"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

const defaultPageSize = 100

// Service handles exporting registry data into seed files.
type Service struct {
	registryService service.RegistryService
	pageSize        int
	readmeOutput    string
}

// NewService creates a new exporter service.
func NewService(registryService service.RegistryService) *Service {
	return &Service{
		registryService: registryService,
		pageSize:        defaultPageSize,
	}
}

// SetPageSize allows tests to override the pagination size used when fetching
// server data from the registry service.
func (s *Service) SetPageSize(size int) {
	if size > 0 {
		s.pageSize = size
	}
}

// SetReadmeOutputPath configures an optional README seed file output path.
func (s *Service) SetReadmeOutputPath(path string) {
	s.readmeOutput = strings.TrimSpace(path)
}

// ExportToPath collects all server definitions from the registry database and
// writes them to the provided file path using the same schema expected by the
// importer (array of apiv0.ServerJSON).
func (s *Service) ExportToPath(ctx context.Context, outputPath string) (int, error) {
	if s.registryService == nil {
		return 0, fmt.Errorf("registry service is not initialized")
	}

	servers, err := s.collectServers(ctx)
	if err != nil {
		return 0, err
	}

	if err := ensureDir(outputPath); err != nil {
		return 0, err
	}

	data, err := json.MarshalIndent(servers, "", "  ")
	if err != nil {
		return 0, fmt.Errorf("failed to marshal servers for export: %w", err)
	}

	if err := os.WriteFile(outputPath, data, 0o644); err != nil {
		return 0, fmt.Errorf("failed to write export file %s: %w", outputPath, err)
	}

	if err := s.writeReadmeSeeds(ctx, servers); err != nil {
		return 0, err
	}

	return len(servers), nil
}

func (s *Service) collectServers(ctx context.Context) ([]*apiv0.ServerJSON, error) {
	var (
		allServers []*apiv0.ServerJSON
		cursor     string
	)

	pageSize := s.pageSize
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}

	for {
		records, nextCursor, err := s.registryService.ListServers(ctx, nil, cursor, pageSize)
		if err != nil {
			return nil, fmt.Errorf("failed to list servers: %w", err)
		}

		for _, record := range records {
			if record == nil {
				continue
			}

			serverCopy := record.Server
			allServers = append(allServers, &serverCopy)
		}

		if nextCursor == "" {
			break
		}

		cursor = nextCursor
	}

	return allServers, nil
}

func ensureDir(outputPath string) error {
	dir := filepath.Dir(outputPath)
	if dir == "" || dir == "." {
		return nil
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create export directory %s: %w", dir, err)
	}

	return nil
}

func (s *Service) writeReadmeSeeds(ctx context.Context, servers []*apiv0.ServerJSON) error {
	if strings.TrimSpace(s.readmeOutput) == "" {
		return nil
	}

	readmes, err := s.collectReadmes(ctx, servers)
	if err != nil {
		return err
	}

	if err := ensureDir(s.readmeOutput); err != nil {
		return err
	}

	if readmes == nil {
		readmes = seed.ReadmeFile{}
	}

	data, err := json.MarshalIndent(readmes, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal README seeds: %w", err)
	}

	if err := os.WriteFile(s.readmeOutput, data, 0o644); err != nil {
		return fmt.Errorf("failed to write README seed file %s: %w", s.readmeOutput, err)
	}

	return nil
}

func (s *Service) collectReadmes(ctx context.Context, servers []*apiv0.ServerJSON) (seed.ReadmeFile, error) {
	result := make(seed.ReadmeFile)

	for _, server := range servers {
		readme, err := s.registryService.GetServerReadmeByVersion(ctx, server.Name, server.Version)
		if err != nil {
			if errors.Is(err, database.ErrNotFound) {
				continue
			}
			return nil, fmt.Errorf("failed to fetch README for %s@%s: %w", server.Name, server.Version, err)
		}
		if readme == nil || len(readme.Content) == 0 {
			continue
		}

		contentType := readme.ContentType
		if contentType == "" {
			contentType = "text/markdown"
		}

		entry := seed.EncodeReadme(readme.Content, contentType)
		result[seed.Key(server.Name, server.Version)] = entry
	}

	return result, nil
}
