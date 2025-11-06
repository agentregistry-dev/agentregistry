package exporter

import (
    "context"
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"

    "github.com/agentregistry-dev/agentregistry/internal/registry/service"
    apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

const defaultPageSize = 100

// Service handles exporting registry data into seed files.
type Service struct {
    registryService service.RegistryService
    pageSize        int
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


