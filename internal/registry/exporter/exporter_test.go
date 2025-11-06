package exporter

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	skillmodels "github.com/agentregistry-dev/agentregistry/internal/models"
	"github.com/agentregistry-dev/agentregistry/internal/registry/database"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

func TestExportToPath_WritesSeedFile(t *testing.T) {
	stub := &stubRegistryService{
		pages: map[string][]*apiv0.ServerResponse{
			"": {
				{Server: apiv0.ServerJSON{Name: "namespace/server-one", Version: "1.0.0"}},
			},
			"cursor-1": {
				{Server: apiv0.ServerJSON{Name: "namespace/server-two", Version: "0.2.0"}},
			},
		},
		next: map[string]string{
			"":         "cursor-1",
			"cursor-1": "",
		},
	}

	service := NewService(stub)
	service.SetPageSize(1)

	outputDir := t.TempDir()
	outputPath := filepath.Join(outputDir, "seed.json")

	count, err := service.ExportToPath(context.Background(), outputPath)
	if err != nil {
		t.Fatalf("ExportToPath returned error: %v", err)
	}

	if count != 2 {
		t.Fatalf("expected 2 servers to be exported, got %d", count)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("failed to read export file: %v", err)
	}

	var exported []apiv0.ServerJSON
	if err := json.Unmarshal(data, &exported); err != nil {
		t.Fatalf("failed to unmarshal export file: %v", err)
	}

	if len(exported) != 2 {
		t.Fatalf("expected 2 servers in export file, got %d", len(exported))
	}

	if exported[0].Name != "namespace/server-one" || exported[1].Name != "namespace/server-two" {
		t.Fatalf("unexpected server names: %+v", exported)
	}
}

func TestExportToPath_PropagatesListError(t *testing.T) {
	stub := &stubRegistryService{listErr: errors.New("boom")}
	service := NewService(stub)

	_, err := service.ExportToPath(context.Background(), filepath.Join(t.TempDir(), "out.json"))
	if err == nil {
		t.Fatal("expected ExportToPath to return an error, got nil")
	}
}

// stubRegistryService implements service.RegistryService for tests, only supporting
// the ListServers method required by the exporter.
type stubRegistryService struct {
	pages   map[string][]*apiv0.ServerResponse
	next    map[string]string
	listErr error
}

func (s *stubRegistryService) ListServers(ctx context.Context, filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error) {
	if s.listErr != nil {
		return nil, "", s.listErr
	}

	page := s.pages[cursor]
	next := s.next[cursor]

	return page, next, nil
}

func (*stubRegistryService) GetServerByName(ctx context.Context, serverName string) (*apiv0.ServerResponse, error) {
	panic("not implemented")
}

func (*stubRegistryService) GetServerByNameAndVersion(ctx context.Context, serverName string, version string) (*apiv0.ServerResponse, error) {
	panic("not implemented")
}

func (*stubRegistryService) GetAllVersionsByServerName(ctx context.Context, serverName string) ([]*apiv0.ServerResponse, error) {
	panic("not implemented")
}

func (*stubRegistryService) CreateServer(ctx context.Context, req *apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
	panic("not implemented")
}

func (*stubRegistryService) UpdateServer(ctx context.Context, serverName, version string, req *apiv0.ServerJSON, newStatus *string) (*apiv0.ServerResponse, error) {
	panic("not implemented")
}

func (*stubRegistryService) StoreServerReadme(ctx context.Context, serverName, version string, content []byte, contentType string) error {
	panic("not implemented")
}

func (*stubRegistryService) GetServerReadmeLatest(ctx context.Context, serverName string) (*database.ServerReadme, error) {
	panic("not implemented")
}

func (*stubRegistryService) GetServerReadmeByVersion(ctx context.Context, serverName, version string) (*database.ServerReadme, error) {
	panic("not implemented")
}

func (*stubRegistryService) ListSkills(ctx context.Context, filter *database.SkillFilter, cursor string, limit int) ([]*skillmodels.SkillResponse, string, error) {
	panic("not implemented")
}

func (*stubRegistryService) GetSkillByName(ctx context.Context, skillName string) (*skillmodels.SkillResponse, error) {
	panic("not implemented")
}

func (*stubRegistryService) GetSkillByNameAndVersion(ctx context.Context, skillName string, version string) (*skillmodels.SkillResponse, error) {
	panic("not implemented")
}

func (*stubRegistryService) GetAllVersionsBySkillName(ctx context.Context, skillName string) ([]*skillmodels.SkillResponse, error) {
	panic("not implemented")
}

func (*stubRegistryService) CreateSkill(ctx context.Context, req *skillmodels.SkillJSON) (*skillmodels.SkillResponse, error) {
	panic("not implemented")
}
