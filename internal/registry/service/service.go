package service

import (
	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	"github.com/agentregistry-dev/agentregistry/internal/registry/embeddings"
	serviceset "github.com/agentregistry-dev/agentregistry/internal/registry/service/set"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

type Set = serviceset.Set

type SetDependencies = serviceset.Dependencies

func NewSet(deps SetDependencies) *Set {
	return serviceset.New(deps)
}

func NewRegistryService(storeDB database.ServiceDatabase, cfg *config.Config, embeddingProvider embeddings.Provider) *Set {
	return NewSet(SetDependencies{
		StoreDB:            storeDB,
		Config:             cfg,
		EmbeddingsProvider: embeddingProvider,
	})
}
