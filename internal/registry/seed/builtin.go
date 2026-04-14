package seed

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	serversvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/server"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

//go:embed seed.json
var builtinSeedData []byte

//go:embed seed-readme.json
var builtinReadmeData []byte

func ImportBuiltinSeedData(ctx context.Context, registry serversvc.Registry) error {
	servers, err := loadSeedData(builtinSeedData)
	if err != nil {
		return err
	}

	readmes, err := loadReadmeSeedData(builtinReadmeData)
	if err != nil {
		return err
	}

	for _, srv := range servers {
		importServer(
			ctx,
			registry,
			srv,
			readmes,
		)
	}

	return nil
}

func loadSeedData(data []byte) ([]*apiv0.ServerJSON, error) {
	var servers []*apiv0.ServerJSON
	if err := json.Unmarshal(data, &servers); err != nil {
		return nil, fmt.Errorf("failed to parse seed data: %w", err)
	}

	return servers, nil
}

func loadReadmeSeedData(data []byte) (ReadmeFile, error) {
	var readmes ReadmeFile
	if err := json.Unmarshal(data, &readmes); err != nil {
		return nil, fmt.Errorf("failed to parse README seed data: %w", err)
	}
	return readmes, nil
}

func importServer(
	ctx context.Context,
	registry serversvc.Registry,
	srv *apiv0.ServerJSON,
	readmes ReadmeFile,
) {
	_, err := registry.PublishServer(ctx, srv)
	if err != nil {
		// If duplicate version and update is enabled, try update path
		if !errors.Is(err, database.ErrInvalidVersion) {
			slog.Error("failed to create server", "name", srv.Name, "error", err)
			return
		}
	}
	slog.Info("imported server", "name", srv.Name, "version", srv.Version)

	entry, ok := readmes[Key(srv.Name, srv.Version)]
	if !ok {
		return
	}

	content, contentType, err := entry.Decode()
	if err != nil {
		slog.Warn("invalid README seed", "name", srv.Name, "version", srv.Version, "error", err)
		return
	}

	if len(content) > 0 {
		if err := registry.SetServerReadme(ctx, srv.Name, srv.Version, content, contentType); err != nil {
			slog.Warn("storing README failed", "name", srv.Name, "version", srv.Version, "error", err)
		}
		slog.Info("stored README", "name", srv.Name, "version", srv.Version)
	}
}
