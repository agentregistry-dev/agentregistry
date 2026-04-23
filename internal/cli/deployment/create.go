package deployment

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"strings"

	cliCommon "github.com/agentregistry-dev/agentregistry/internal/cli/common"
	cliUtils "github.com/agentregistry-dev/agentregistry/internal/cli/utils"
	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/internal/constants"
	arv0 "github.com/agentregistry-dev/agentregistry/pkg/api/v0"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/spf13/cobra"
)

var CreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a deployment",
	Long: `Create a deployment for an agent or MCP server from the registry.

Example:
  arctl deployments create my-agent --type agent --version latest
  arctl deployments create my-mcp-server --type mcp --version 1.2.3
  arctl deployments create my-agent --type agent --provider-id kubernetes-default`,
	Args:          cobra.ExactArgs(1),
	RunE:          runCreate,
	SilenceUsage:  true,
	SilenceErrors: false,
}

func init() {
	CreateCmd.Flags().String("type", "", "Resource type to deploy (agent or mcp)")
	CreateCmd.Flags().String("version", "latest", "Version to deploy")
	CreateCmd.Flags().String("provider-id", "", "Deployment target provider ID (defaults to local when omitted)")
	CreateCmd.Flags().String("namespace", "", "Kubernetes namespace for deployment")
	CreateCmd.Flags().Bool("wait", true, "Wait for the deployment to become ready before returning")
	CreateCmd.Flags().Bool("prefer-remote", false, "Prefer using a remote source when available")
	CreateCmd.Flags().StringArrayP("env", "e", []string{}, "Environment variables to set (KEY=VALUE)")
	CreateCmd.Flags().StringArrayP("arg", "a", []string{}, "Runtime arguments for MCP servers (KEY=VALUE)")
	CreateCmd.Flags().StringArray("header", []string{}, "HTTP headers for remote MCP servers (KEY=VALUE)")

	_ = CreateCmd.MarkFlagRequired("type")
}

// providerAPIKeys maps model providers to their expected API key env var names.
var providerAPIKeys = map[string]string{
	"openai":      "OPENAI_API_KEY",
	"anthropic":   "ANTHROPIC_API_KEY",
	"azureopenai": "AZUREOPENAI_API_KEY",
	"gemini":      "GOOGLE_API_KEY",
}

func runCreate(cmd *cobra.Command, args []string) error {
	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	name := args[0]
	resourceType, _ := cmd.Flags().GetString("type")
	version, _ := cmd.Flags().GetString("version")
	providerID, _ := cmd.Flags().GetString("provider-id")
	namespace, _ := cmd.Flags().GetString("namespace")
	wait, _ := cmd.Flags().GetBool("wait")
	preferRemote, _ := cmd.Flags().GetBool("prefer-remote")
	envFlags, _ := cmd.Flags().GetStringArray("env")
	argFlags, _ := cmd.Flags().GetStringArray("arg")
	headerFlags, _ := cmd.Flags().GetStringArray("header")

	resourceType = strings.ToLower(resourceType)
	if resourceType != "agent" && resourceType != "mcp" {
		return fmt.Errorf("invalid --type %q: must be 'agent' or 'mcp'", resourceType)
	}

	if version == "" {
		version = "latest"
	}
	if providerID == "" {
		providerID = "local"
	}

	envMap, err := cliUtils.ParseEnvFlags(envFlags)
	if err != nil {
		return err
	}

	// Parse --arg flags (MCP-specific, prefixed with ARG_)
	for _, arg := range argFlags {
		parts := strings.SplitN(arg, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid arg format (expected KEY=VALUE): %s", arg)
		}
		envMap["ARG_"+parts[0]] = parts[1]
	}

	// Parse --header flags (MCP-specific, prefixed with HEADER_)
	for _, header := range headerFlags {
		parts := strings.SplitN(header, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid header format (expected KEY=VALUE): %s", header)
		}
		envMap["HEADER_"+parts[0]] = parts[1]
	}

	if namespace != "" {
		envMap[constants.EnvKagentNamespace] = namespace
	}

	switch resourceType {
	case "agent":
		return createAgentDeployment(cmd.Context(), name, version, envMap, providerID, namespace, wait)
	case "mcp":
		return createMCPDeployment(cmd.Context(), name, version, envMap, providerID, namespace, preferRemote, wait)
	}
	return nil
}

func createAgentDeployment(ctx context.Context, name, version string, envMap map[string]string, providerID, namespace string, wait bool) error {
	agent, err := client.GetTyped(
		ctx,
		apiClient,
		v1alpha1.KindAgent,
		v1alpha1.DefaultNamespace,
		name,
		resolveRequestedVersion(version),
		func() *v1alpha1.Agent { return &v1alpha1.Agent{} },
	)
	if err != nil {
		return fmt.Errorf("failed to fetch agent %q: %w", name, err)
	}
	if _, err := client.GetTyped(
		ctx,
		apiClient,
		v1alpha1.KindProvider,
		v1alpha1.DefaultNamespace,
		providerID,
		"",
		func() *v1alpha1.Provider { return &v1alpha1.Provider{} },
	); err != nil {
		return fmt.Errorf("failed to fetch provider %q: %w", providerID, err)
	}

	if err := validateAPIKey(agent.Spec.ModelProvider, envMap); err != nil {
		return err
	}

	config := buildAgentDeployConfig(agent, envMap)
	if namespace != "" {
		config[constants.EnvKagentNamespace] = namespace
	}

	deployment := newDeploymentResource(v1alpha1.KindAgent, name, agent.Metadata.Version, providerID, config, false)
	if err := applyDeploymentResource(ctx, deployment); err != nil {
		return err
	}

	deploymentID := cliCommon.DeploymentID(deployment.Metadata.Namespace, deployment.Metadata.Name, deployment.Metadata.Version)

	if providerID != "local" && wait {
		fmt.Printf("Waiting for agent '%s' to become ready...\n", name)
		if err := cliCommon.WaitForDeploymentReady(apiClient, deploymentID); err != nil {
			return err
		}
	}

	ns := namespace
	if ns == "" {
		ns = "(default)"
	}
	fmt.Printf("Agent '%s' version '%s' deployed to providerId=%s in namespace '%s' (deployment %s)\n", name, agent.Metadata.Version, providerID, ns, deploymentID)
	return nil
}

func createMCPDeployment(ctx context.Context, name, version string, envMap map[string]string, providerID, namespace string, preferRemote bool, wait bool) error {
	server, err := client.GetTyped(
		ctx,
		apiClient,
		v1alpha1.KindMCPServer,
		v1alpha1.DefaultNamespace,
		name,
		resolveRequestedVersion(version),
		func() *v1alpha1.MCPServer { return &v1alpha1.MCPServer{} },
	)
	if err != nil {
		return fmt.Errorf("failed to fetch server %q: %w", name, err)
	}
	if _, err := client.GetTyped(
		ctx,
		apiClient,
		v1alpha1.KindProvider,
		v1alpha1.DefaultNamespace,
		providerID,
		"",
		func() *v1alpha1.Provider { return &v1alpha1.Provider{} },
	); err != nil {
		return fmt.Errorf("failed to fetch provider %q: %w", providerID, err)
	}

	fmt.Println("\nDeploying server...")
	deployment := newDeploymentResource(v1alpha1.KindMCPServer, name, server.Metadata.Version, providerID, envMap, preferRemote)
	if err := applyDeploymentResource(ctx, deployment); err != nil {
		return err
	}
	deploymentID := cliCommon.DeploymentID(deployment.Metadata.Namespace, deployment.Metadata.Name, deployment.Metadata.Version)

	if providerID != "local" && wait {
		fmt.Printf("Waiting for server '%s' to become ready...\n", name)
		if err := cliCommon.WaitForDeploymentReady(apiClient, deploymentID); err != nil {
			return err
		}
	}

	fmt.Printf("\nDeployed %s (%s) with providerId=%s (deployment %s)\n", name, cliCommon.FormatVersionForDisplay(server.Metadata.Version), providerID, deploymentID)
	if namespace != "" {
		fmt.Printf("Namespace: %s\n", namespace)
	}
	if len(envMap) > 0 {
		fmt.Printf("Deployment Env: %d setting(s)\n", len(envMap))
	}
	if providerID == "local" {
		fmt.Printf("\nServer deployment recorded. The registry will reconcile containers automatically.\n")
		fmt.Printf("Agent Gateway endpoint: http://localhost:%s/mcp\n", cliCommon.DefaultAgentGatewayPort)
	}

	return nil
}

// validateAPIKey checks that the required API key for the given model provider is set.
func validateAPIKey(modelProvider string, extraEnv map[string]string) error {
	envVar, ok := providerAPIKeys[strings.ToLower(modelProvider)]
	if !ok || envVar == "" {
		return nil
	}
	if v, exists := extraEnv[envVar]; exists && v != "" {
		return nil
	}
	if os.Getenv(envVar) == "" {
		return fmt.Errorf("required API key %s not set for model provider %s", envVar, modelProvider)
	}
	return nil
}

// buildAgentDeployConfig creates the configuration map with all necessary environment variables.
func buildAgentDeployConfig(agent *v1alpha1.Agent, envOverrides map[string]string) map[string]string {
	config := make(map[string]string)
	maps.Copy(config, envOverrides)

	if agent == nil {
		return config
	}

	if envVar, ok := providerAPIKeys[strings.ToLower(agent.Spec.ModelProvider)]; ok && envVar != "" {
		if _, exists := config[envVar]; !exists {
			if value := os.Getenv(envVar); value != "" {
				config[envVar] = value
			}
		}
	}

	if agent.Spec.TelemetryEndpoint != "" {
		config["OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"] = agent.Spec.TelemetryEndpoint
	}

	return config
}

func resolveRequestedVersion(version string) string {
	if version == "" || version == "latest" {
		return ""
	}
	return version
}

func newDeploymentResource(targetKind, targetName, targetVersion, providerID string, env map[string]string, preferRemote bool) *v1alpha1.Deployment {
	return &v1alpha1.Deployment{
		TypeMeta: v1alpha1.TypeMeta{
			APIVersion: v1alpha1.GroupVersion,
			Kind:       v1alpha1.KindDeployment,
		},
		Metadata: v1alpha1.ObjectMeta{
			Namespace: v1alpha1.DefaultNamespace,
			Name:      cliCommon.DeploymentResourceName(targetName, providerID),
			Version:   targetVersion,
		},
		Spec: v1alpha1.DeploymentSpec{
			TargetRef: v1alpha1.ResourceRef{
				Kind:      targetKind,
				Name:      targetName,
				Version:   targetVersion,
				Namespace: v1alpha1.DefaultNamespace,
			},
			ProviderRef: v1alpha1.ResourceRef{
				Kind:      v1alpha1.KindProvider,
				Name:      providerID,
				Namespace: v1alpha1.DefaultNamespace,
			},
			DesiredState:   v1alpha1.DesiredStateDeployed,
			Env:            env,
			PreferRemote:   preferRemote,
			ProviderConfig: nil,
		},
	}
}

func applyDeploymentResource(ctx context.Context, deployment *v1alpha1.Deployment) error {
	if deployment == nil {
		return fmt.Errorf("deployment is nil")
	}

	body, err := json.Marshal(deployment)
	if err != nil {
		return fmt.Errorf("marshal deployment: %w", err)
	}

	results, err := apiClient.Apply(ctx, body, client.ApplyOpts{})
	if err != nil {
		return fmt.Errorf("apply deployment: %w", err)
	}
	if len(results) == 0 {
		return fmt.Errorf("apply deployment: empty response")
	}
	for _, result := range results {
		if result.Status == arv0.ApplyStatusFailed {
			return fmt.Errorf("apply deployment %s/%s: %s", result.Kind, result.Name, result.Error)
		}
	}
	return nil
}
