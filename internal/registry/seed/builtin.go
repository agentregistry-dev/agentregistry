package seed

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"

	"log"

	"github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
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
			log.Printf("Failed to create server %s: %v", srv.Name, err)
			return
		}
	}
	log.Printf("Imported server %s@%s", srv.Name, srv.Version)

	entry, ok := readmes[Key(srv.Name, srv.Version)]
	if !ok {
		return
	}

	content, contentType, err := entry.Decode()
	if err != nil {
		log.Printf("Warning: invalid README seed for %s@%s: %v", srv.Name, srv.Version, err)
		return
	}

	if len(content) > 0 {
		if err := registry.StoreServerReadme(ctx, srv.Name, srv.Version, content, contentType); err != nil {
			log.Printf("Warning: storing README failed for %s@%s: %v", srv.Name, srv.Version, err)
		}
		log.Printf("Stored README for %s@%s", srv.Name, srv.Version)
	}
}
