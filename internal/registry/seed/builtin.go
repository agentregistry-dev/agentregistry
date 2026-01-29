package seed

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/internal/registry/logging"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

//go:embed seed.json
var builtinSeedData []byte

//go:embed seed-readme.json
var builtinReadmeData []byte

func ImportBuiltinSeedData(ctx context.Context, registry service.RegistryService) error {
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
	registry service.RegistryService,
	srv *apiv0.ServerJSON,
	readmes ReadmeFile,
) {
	_, err := registry.CreateServer(ctx, srv)
	if err != nil {
		// If duplicate version and update is enabled, try update path
		if !errors.Is(err, database.ErrInvalidVersion) {
			logging.Log(ctx, logging.SystemLog, zapcore.ErrorLevel, "Failed to create server", zap.String("server_name", srv.Name), zap.Error(err))
			return
		}
	}
	logging.Log(ctx, logging.SystemLog, zapcore.InfoLevel, "Imported server", zap.String("server_name", srv.Name), zap.String("server_version", srv.Version))

	entry, ok := readmes[Key(srv.Name, srv.Version)]
	if !ok {
		return
	}

	content, contentType, err := entry.Decode()
	if err != nil {
		logging.Log(ctx, logging.SystemLog, zapcore.WarnLevel, "invalid README seed", zap.String("server_name", srv.Name), zap.String("server_version", srv.Version), zap.Error(err))
		return
	}

	if len(content) > 0 {
		if err := registry.StoreServerReadme(ctx, srv.Name, srv.Version, content, contentType); err != nil {
			logging.Log(ctx, logging.SystemLog, zapcore.WarnLevel, "storing README failed", zap.String("server_name", srv.Name), zap.String("server_version", srv.Version), zap.Error(err))
		}
		logging.Log(ctx, logging.SystemLog, zapcore.InfoLevel, "Stored README", zap.String("server_name", srv.Name), zap.String("server_version", srv.Version))
	}
}
