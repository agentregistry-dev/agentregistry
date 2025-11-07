package exporter

import (
    "context"
    "encoding/json"
    "errors"
    "os"
    "path/filepath"
    "strings"
    "testing"
    "time"

    skillmodels "github.com/agentregistry-dev/agentregistry/internal/models"
    "github.com/agentregistry-dev/agentregistry/internal/registry/database"
    "github.com/agentregistry-dev/agentregistry/internal/registry/seed"
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
        readmes: map[string]*database.ServerReadme{
            seed.Key("namespace/server-one", "1.0.0"): {
                ServerName:  "namespace/server-one",
                Version:     "1.0.0",
                Content:     []byte("# Server One\n"),
                ContentType: "text/markdown",
                SizeBytes:   len([]byte("# Server One\n")),
                FetchedAt:   time.Now(),
            },
            seed.Key("namespace/server-two", "0.2.0"): {
                ServerName:  "namespace/server-two",
                Version:     "0.2.0",
                Content:     []byte("# Server Two\n"),
                ContentType: "text/markdown",
                SizeBytes:   len([]byte("# Server Two\n")),
                FetchedAt:   time.Now(),
            },
        },
	}

	service := NewService(stub)
	service.SetPageSize(1)

	outputDir := t.TempDir()
	outputPath := filepath.Join(outputDir, "seed.json")
    readmePath := filepath.Join(outputDir, "readmes.json")
    service.SetReadmeOutputPath(readmePath)

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

    readmeData, err := os.ReadFile(readmePath)
    if err != nil {
        t.Fatalf("failed to read readme export file: %v", err)
    }

    var exportedReadmes seed.ReadmeFile
    if len(readmeData) > 0 {
        if err := json.Unmarshal(readmeData, &exportedReadmes); err != nil {
            t.Fatalf("failed to unmarshal readme export: %v", err)
        }
    }

    entry, ok := exportedReadmes[seed.Key("namespace/server-one", "1.0.0")]
    if !ok {
        t.Fatalf("expected readme entry for server-one")
    }
    decoded, contentType, err := entry.Decode()
    if err != nil {
        t.Fatalf("failed to decode readme entry: %v", err)
    }
    if contentType != "text/markdown" {
        t.Fatalf("unexpected content type: %s", contentType)
    }
    if string(decoded) != "# Server One\n" {
        t.Fatalf("unexpected readme content: %q", string(decoded))
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
    readmes map[string]*database.ServerReadme
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

func (s *stubRegistryService) StoreServerReadme(ctx context.Context, serverName, version string, content []byte, contentType string) error {
    if s.readmes == nil {
        s.readmes = make(map[string]*database.ServerReadme)
    }
    s.readmes[seed.Key(serverName, version)] = &database.ServerReadme{
        ServerName:  serverName,
        Version:     version,
        Content:     append([]byte(nil), content...),
        ContentType: contentType,
        SizeBytes:   len(content),
        FetchedAt:   time.Now(),
    }
    return nil
}

func (s *stubRegistryService) GetServerReadmeLatest(ctx context.Context, serverName string) (*database.ServerReadme, error) {
    for key, readme := range s.readmes {
        if strings.HasPrefix(key, serverName+"@") {
            return readme, nil
        }
    }
    return nil, database.ErrNotFound
}

func (s *stubRegistryService) GetServerReadmeByVersion(ctx context.Context, serverName, version string) (*database.ServerReadme, error) {
    if readme, ok := s.readmes[seed.Key(serverName, version)]; ok {
        return readme, nil
    }
    return nil, database.ErrNotFound
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
