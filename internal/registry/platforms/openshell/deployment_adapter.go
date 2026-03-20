package openshell

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/registry/platforms/utils"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

const (
	sandboxReadyTimeout = 120 * time.Second
	sandboxPollInterval = 2 * time.Second
	// Phase enum string values from the generated proto.
	sandboxPhaseReady = "SANDBOX_PHASE_READY"
	sandboxPhaseError = "SANDBOX_PHASE_ERROR"
)

type openshellDeploymentAdapter struct {
	registry    service.RegistryService
	client      Client
	clientOnce  sync.Once
	clientErr   error
	gatewayName string
}

// NewOpenShellDeploymentAdapter creates a new deployment adapter for the OpenShell platform.
// The client can be nil — it will be lazily created on first deploy using env vars or
// gateway filesystem config.
func NewOpenShellDeploymentAdapter(registry service.RegistryService, client Client) *openshellDeploymentAdapter {
	return &openshellDeploymentAdapter{
		registry: registry,
		client:   client,
	}
}

// getClient returns the gRPC client, creating it lazily if needed.
func (a *openshellDeploymentAdapter) getClient() (Client, error) {
	if a.client != nil {
		return a.client, nil
	}
	a.clientOnce.Do(func() {
		slog.Info("openshell: lazily creating gRPC client")
		c, err := NewGRPCClient(a.gatewayName)
		if err != nil {
			slog.Error("openshell: failed to create gRPC client", "error", err)
			a.clientErr = fmt.Errorf("openshell client not configured: %w", err)
			return
		}
		slog.Info("openshell: gRPC client created successfully")
		a.client = c
	})
	if a.clientErr != nil {
		return nil, a.clientErr
	}
	return a.client, nil
}

func (a *openshellDeploymentAdapter) Platform() string { return "openshell" }

func (a *openshellDeploymentAdapter) SupportedResourceTypes() []string {
	return []string{"mcp", "agent"}
}

func (a *openshellDeploymentAdapter) Deploy(ctx context.Context, req *models.Deployment) (*models.DeploymentActionResult, error) {
	slog.Info("openshell: deploy started", "server", req.ServerName, "provider", req.ProviderID)
	client, err := a.getClient()
	if err != nil {
		return nil, err
	}
	slog.Info("openshell: client ready")
	if err := utils.ValidateDeploymentRequest(req, false); err != nil {
		return nil, err
	}

	sandboxName := sandboxNameForDeployment(req)
	image, env, providers, err := a.resolveDeploymentSpec(ctx, req)
	if err != nil {
		return nil, err
	}
	slog.Info("openshell: resolved spec", "sandbox", sandboxName, "image", image)

	opts := CreateSandboxOpts{
		Name:      sandboxName,
		Image:     image,
		Env:       env,
		Providers: providers,
	}

	slog.Info("openshell: calling CreateSandbox")
	if _, err := client.CreateSandbox(ctx, opts); err != nil {
		return nil, fmt.Errorf("create openshell sandbox: %w", err)
	}
	slog.Info("openshell: sandbox created, waiting for ready")

	if err := a.waitForReady(ctx, client, sandboxName); err != nil {
		return nil, err
	}

	return &models.DeploymentActionResult{Status: models.DeploymentStatusDeployed}, nil
}

func (a *openshellDeploymentAdapter) Undeploy(_ context.Context, deployment *models.Deployment) error {
	client, err := a.getClient()
	if err != nil {
		return err
	}
	if err := utils.ValidateDeploymentRequest(deployment, true); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sandboxName := sandboxNameForDeployment(deployment)
	if err := client.DeleteSandbox(ctx, sandboxName); err != nil {
		return fmt.Errorf("delete openshell sandbox: %w", err)
	}
	return nil
}

func (a *openshellDeploymentAdapter) GetLogs(ctx context.Context, deployment *models.Deployment) ([]string, error) {
	client, err := a.getClient()
	if err != nil {
		return nil, err
	}
	if deployment == nil {
		return nil, fmt.Errorf("deployment is required: %w", database.ErrInvalidInput)
	}
	sandboxName := sandboxNameForDeployment(deployment)
	return client.GetSandboxLogs(ctx, sandboxName)
}

func (a *openshellDeploymentAdapter) Cancel(ctx context.Context, deployment *models.Deployment) error {
	client, err := a.getClient()
	if err != nil {
		return err
	}
	if deployment == nil {
		return fmt.Errorf("deployment is required: %w", database.ErrInvalidInput)
	}
	sandboxName := sandboxNameForDeployment(deployment)
	return client.DeleteSandbox(ctx, sandboxName)
}

func (a *openshellDeploymentAdapter) Discover(_ context.Context, _ string) ([]*models.Deployment, error) {
	return []*models.Deployment{}, nil
}

// resolveDeploymentSpec extracts the container image, environment variables, and provider
// names from the deployment by resolving the resource in the registry.
func (a *openshellDeploymentAdapter) resolveDeploymentSpec(
	ctx context.Context,
	deployment *models.Deployment,
) (image string, env map[string]string, providers []string, err error) {
	resourceType := strings.ToLower(strings.TrimSpace(deployment.ResourceType))
	switch resourceType {
	case "mcp":
		server, sErr := utils.BuildPlatformMCPServer(ctx, a.registry, deployment, "")
		if sErr != nil {
			return "", nil, nil, sErr
		}
		if server.Local != nil && server.Local.Deployment.Image != "" {
			return server.Local.Deployment.Image, mergeEnv(deployment.Env, server.Local.Deployment.Env), nil, nil
		}
		return "", nil, nil, fmt.Errorf("openshell requires a container image for MCP server %s", server.Name)

	case "agent":
		resolved, rErr := utils.ResolveAgent(ctx, a.registry, deployment, "")
		if rErr != nil {
			return "", nil, nil, rErr
		}
		return resolved.Agent.Deployment.Image, mergeEnv(deployment.Env, resolved.Agent.Deployment.Env), nil, nil

	default:
		return "", nil, nil, fmt.Errorf("invalid resource type %q: %w", deployment.ResourceType, database.ErrInvalidInput)
	}
}

// waitForReady polls GetSandbox until the sandbox reaches the Ready phase or times out.
func (a *openshellDeploymentAdapter) waitForReady(ctx context.Context, client Client, sandboxName string) error {
	deadline := time.Now().Add(sandboxReadyTimeout)
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for openshell sandbox %s to become ready", sandboxName)
		}

		info, err := client.GetSandbox(ctx, sandboxName)
		if err != nil {
			slog.Warn("polling openshell sandbox", "name", sandboxName, "error", err)
		} else {
			slog.Info("openshell: sandbox phase", "name", sandboxName, "phase", info.Phase)
			switch info.Phase {
			case sandboxPhaseReady:
				return nil
			case sandboxPhaseError:
				return fmt.Errorf("openshell sandbox %s entered error phase", sandboxName)
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sandboxPollInterval):
		}
	}
}

// sandboxNameForDeployment derives a K8s-compatible sandbox name from the deployment.
func sandboxNameForDeployment(deployment *models.Deployment) string {
	if deployment == nil {
		return ""
	}
	return utils.GenerateInternalNameForDeployment(deployment.ServerName, deployment.ID)
}

// mergeEnv combines base and override env maps, with override taking precedence.
func mergeEnv(base, override map[string]string) map[string]string {
	result := make(map[string]string, len(base)+len(override))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range override {
		result[k] = v
	}
	return result
}
