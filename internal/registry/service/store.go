package service

import (
	"context"
	"errors"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

type storeBundle struct {
	servers     database.ServerStore
	agents      database.AgentStore
	skills      database.SkillStore
	prompts     database.PromptStore
	providers   database.ProviderStore
	deployments database.DeploymentStore
}

func bundleFromStore(store database.Store) storeBundle {
	return storeBundle{
		servers:     store,
		agents:      store,
		skills:      store,
		prompts:     store,
		providers:   store,
		deployments: store,
	}
}

func (s *registryServiceImpl) serviceDatabase() database.ServiceDatabase {
	return s.storeDB
}

func (s *registryServiceImpl) readStores() storeBundle {
	var stores storeBundle
	if storeDB := s.serviceDatabase(); storeDB != nil {
		stores = bundleFromStore(storeDB)
	}
	if s.serverRepo != nil {
		stores.servers = s.serverRepo
	}
	if s.agentRepo != nil {
		stores.agents = s.agentRepo
	}
	if s.skillRepo != nil {
		stores.skills = s.skillRepo
	}
	if s.promptRepo != nil {
		stores.prompts = s.promptRepo
	}
	if s.providerRepo != nil {
		stores.providers = s.providerRepo
	}
	if s.deploymentRepo != nil {
		stores.deployments = s.deploymentRepo
	}
	return stores
}

func (s *registryServiceImpl) inTransaction(ctx context.Context, fn func(context.Context, storeBundle) error) error {
	storeDB := s.serviceDatabase()
	if storeDB == nil {
		return errors.New("service database is not configured")
	}

	return storeDB.InTransaction(ctx, func(txCtx context.Context, store database.Store) error {
		return fn(txCtx, bundleFromStore(store))
	})
}

func inTransactionT[T any](ctx context.Context, s *registryServiceImpl, fn func(context.Context, storeBundle) (T, error)) (T, error) {
	var result T
	var fnErr error

	err := s.inTransaction(ctx, func(txCtx context.Context, stores storeBundle) error {
		result, fnErr = fn(txCtx, stores)
		return fnErr
	})
	if err != nil {
		var zero T
		return zero, err
	}

	return result, nil
}
