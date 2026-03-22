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
	// openshellKagentForwardPort is the in-sandbox port for kagent-adk HTTP; users forward this from the host.
	openshellKagentForwardPort = 9999
	// openshellSandboxCommandEnv is the env var the supervisor reads to exec the workload; the
	// container entrypoint remains /opt/openshell/bin/openshell-sandbox (setting container.command
	// in a pod patch does not replace that).
	openshellSandboxCommandEnv = "OPENSHELL_SANDBOX_COMMAND"
)

// openshellProviderMapping maps agent model-provider slugs to their
// corresponding OpenShell provider type and credential env vars.
// Supported OpenShell types: claude, codex, generic, github, gitlab,
// nvidia, openai, opencode.
// See https://docs.nvidia.com/openshell/latest/sandboxes/manage-providers.html
type openshellProviderSpec struct {
	Type           string   // OpenShell provider type
	CredentialKeys []string // env var names that carry API credentials
}

var openshellProviderMapping = map[string]openshellProviderSpec{
	"anthropic": {Type: "claude", CredentialKeys: []string{"ANTHROPIC_API_KEY", "CLAUDE_API_KEY"}},
	"openai":    {Type: "openai", CredentialKeys: []string{"OPENAI_API_KEY"}},
	"nvidia":    {Type: "nvidia", CredentialKeys: []string{"NVIDIA_API_KEY"}},
	"google":    {Type: "generic", CredentialKeys: []string{"GOOGLE_API_KEY", "GEMINI_API_KEY"}},
	"gemini":    {Type: "generic", CredentialKeys: []string{"GOOGLE_API_KEY", "GEMINI_API_KEY"}},
	"mistral":   {Type: "generic", CredentialKeys: []string{"MISTRAL_API_KEY"}},
	"cohere":    {Type: "generic", CredentialKeys: []string{"COHERE_API_KEY"}},
}

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
	if err := utils.ValidateDeploymentRequest(req, false); err != nil {
		return nil, err
	}
	slog.Info("openshell: deploy started", "server", req.ServerName, "provider", req.ProviderID)
	client, err := a.getClient()
	if err != nil {
		return nil, err
	}
	slog.Info("openshell: client ready")

	sandboxName := sandboxNameForDeployment(req)
	image, env, providers, err := a.resolveDeploymentSpec(ctx, req)
	if err != nil {
		return nil, err
	}
	slog.Info("openshell: resolved spec", "sandbox", sandboxName, "image", image, "providers", providers)

	resourceType := strings.ToLower(strings.TrimSpace(req.ResourceType))

	if resourceType == "agent" {
		agentName := strings.TrimSpace(req.ServerName)
		env = mergeEnv(env, map[string]string{
			"KAGENT_URL":       "placeholder",
			"KAGENT_NAME":      agentName,
			"KAGENT_NAMESPACE": "placeholder",
		})
	}

	if err := a.ensureProviders(ctx, client, providers, env); err != nil {
		return nil, err
	}

	opts := CreateSandboxOpts{
		Name:      sandboxName,
		Image:     image,
		Env:       env,
		Providers: providers,
	}
	if resourceType == "agent" {
		agentName := strings.TrimSpace(req.ServerName)
		modelProvider := strings.TrimSpace(env["MODEL_PROVIDER"])
		cmd := kagentADKOpenshellCommand(agentName, openshellKagentForwardPort)
		opts.Policy = kagentADKSandboxPolicy(modelProvider)
		opts.Command = cmd
		opts.Env = mergeEnv(opts.Env, map[string]string{
			openshellSandboxCommandEnv: joinOpenshellSandboxWorkload(cmd),
		})
	}

	slog.Info("openshell: calling CreateSandbox")
	if _, err := client.CreateSandbox(ctx, opts); err != nil {
		return nil, fmt.Errorf("create openshell sandbox: %w", err)
	}
	slog.Info("openshell: sandbox created, waiting for ready")

	if err := a.waitForReady(ctx, client, sandboxName); err != nil {
		return nil, err
	}

	result := &models.DeploymentActionResult{Status: models.DeploymentStatusDeployed}
	if resourceType == "agent" {
		forwardCLI := fmt.Sprintf("openshell forward start %d %s", openshellKagentForwardPort, sandboxName)
		// User-facing hint is persisted on providerMetadata (arctl prints it); slog is for server logs only.
		slog.Info("openshell: agent sandbox ready — forward this port from your machine to reach the agent",
			"command", forwardCLI,
			"sandbox", sandboxName,
			"port", openshellKagentForwardPort,
		)
		meta, mErr := models.UnmarshalFrom(map[string]any{
			"openshellSandboxName": sandboxName,
			"openshellForwardPort": openshellKagentForwardPort,
			"openshellForwardCLI":  forwardCLI,
		})
		if mErr != nil {
			slog.Warn("openshell: could not build provider metadata", "error", mErr)
		} else {
			result.ProviderMetadata = meta
		}
	}
	return result, nil
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
		mergedEnv := mergeEnv(deployment.Env, resolved.Agent.Deployment.Env)
		modelProvider := mergedEnv["MODEL_PROVIDER"]
		if modelProvider != "" {
			providers = append(providers, strings.ToLower(modelProvider))
		}
		return resolved.Agent.Deployment.Image, mergedEnv, providers, nil

	default:
		return "", nil, nil, fmt.Errorf("invalid resource type %q: %w", deployment.ResourceType, database.ErrInvalidInput)
	}
}

// ensureProviders makes sure every named inference provider exists on the
// gateway, creating it with credentials extracted from env if necessary.
func (a *openshellDeploymentAdapter) ensureProviders(ctx context.Context, client Client, providers []string, env map[string]string) error {
	for _, name := range providers {
		spec := resolveProviderSpec(name)
		creds := extractProviderCredentials(spec, env)
		slog.Info("openshell: ensuring provider", "provider", name, "openshell_type", spec.Type, "has_credentials", len(creds) > 0)
		if err := client.EnsureProvider(ctx, name, spec.Type, creds); err != nil {
			return fmt.Errorf("ensure openshell provider %s: %w", name, err)
		}
	}
	return nil
}

// resolveProviderSpec returns the OpenShell provider spec for a model provider
// slug. Unknown providers fall back to the "generic" type.
func resolveProviderSpec(provider string) openshellProviderSpec {
	if spec, ok := openshellProviderMapping[provider]; ok {
		return spec
	}
	return openshellProviderSpec{Type: "generic"}
}

// extractProviderCredentials looks up known API-key env vars for a provider
// spec and returns them as a credentials map suitable for OpenShell.
func extractProviderCredentials(spec openshellProviderSpec, env map[string]string) map[string]string {
	creds := make(map[string]string)
	for _, key := range spec.CredentialKeys {
		if v, exists := env[key]; exists && v != "" {
			creds[key] = v
		}
	}
	return creds
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

// joinOpenshellSandboxWorkload formats argv for OPENSHELL_SANDBOX_COMMAND (space-separated, like
// the default "sleep infinity"). Agent names must not contain spaces.
func joinOpenshellSandboxWorkload(argv []string) string {
	return strings.Join(argv, " ")
}

// kagentADKOpenshellCommand is the process OpenShell runs inside the agent image (see kagent-adk CLI).
func kagentADKOpenshellCommand(agentName string, port int) []string {
	return []string{
		"kagent-adk", "run",
		"--host", "0.0.0.0",
		"--port", fmt.Sprintf("%d", port),
		agentName,
		"--local",
	}
}
