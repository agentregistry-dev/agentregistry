package runtime

import (
	"context"
	_ "embed"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
	"github.com/agentregistry-dev/agentregistry/internal/runtime/translation/api"
	"github.com/agentregistry-dev/agentregistry/internal/runtime/translation/kagent"
	"github.com/agentregistry-dev/agentregistry/internal/runtime/translation/registry"

	v1alpha2 "github.com/kagent-dev/kagent/go/api/v1alpha2"
	kmcpv1alpha1 "github.com/kagent-dev/kmcp/api/v1alpha1"
	"go.yaml.in/yaml/v3"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// fieldManager identifies agentregistry as the field owner for server-side apply.
const fieldManager = "agentregistry"

// scheme contains the API types for controller-runtime client.
var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha2.AddToScheme(scheme))
	utilruntime.Must(kmcpv1alpha1.AddToScheme(scheme))
}

// newClient creates a controller-runtime client with the kagent scheme.
func newClient(verbose bool) (client.Client, error) {
	kubeconfigPath := os.Getenv("KUBECONFIG")
	if kubeconfigPath == "" {
		if home := os.Getenv("HOME"); home != "" {
			kubeconfigPath = filepath.Join(home, ".kube", "config")
		}
	}

	if verbose {
		fmt.Printf("Using kubeconfig: %s\n", kubeconfigPath)
	}

	restConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig from %s: %w", kubeconfigPath, err)
	}

	c, err := client.New(restConfig, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return c, nil
}

// applyResource uses server-side apply to create or update a Kubernetes resource.
func applyResource(ctx context.Context, c client.Client, obj client.Object, verbose bool) error {
	if verbose {
		fmt.Printf("Applying %s %s in namespace %s\n",
			obj.GetObjectKind().GroupVersionKind().Kind,
			obj.GetName(),
			obj.GetNamespace())
	}

	// Server-side apply: single declarative call, no need to check if exists
	if err := c.Patch(ctx, obj, client.Apply, client.FieldOwner(fieldManager), client.ForceOwnership); err != nil {
		return fmt.Errorf("failed to apply %s %s: %w",
			obj.GetObjectKind().GroupVersionKind().Kind, obj.GetName(), err)
	}

	if verbose {
		fmt.Printf("Applied %s %s\n", obj.GetObjectKind().GroupVersionKind().Kind, obj.GetName())
	}
	return nil
}

// deleteResource deletes a Kubernetes resource, ignoring NotFound errors.
func deleteResource(ctx context.Context, c client.Client, obj client.Object) error {
	if err := c.Delete(ctx, obj); client.IgnoreNotFound(err) != nil {
		return err
	}
	return nil
}

type AgentRegistryRuntime interface {
	ReconcileAll(
		ctx context.Context,
		servers []*registry.MCPServerRunRequest,
		agents []*registry.AgentRunRequest,
	) error
}

type agentRegistryRuntime struct {
	registryTranslator registry.Translator
	runtimeTranslator  api.RuntimeTranslator
	runtimeDir         string
	verbose            bool
}

func NewAgentRegistryRuntime(
	registryTranslator registry.Translator,
	translator api.RuntimeTranslator,
	runtimeDir string,
	verbose bool,
) AgentRegistryRuntime {
	return &agentRegistryRuntime{
		registryTranslator: registryTranslator,
		runtimeTranslator:  translator,
		runtimeDir:         runtimeDir,
		verbose:            verbose,
	}
}

func (r *agentRegistryRuntime) ReconcileAll(
	ctx context.Context,
	serverRequests []*registry.MCPServerRunRequest,
	agentRequests []*registry.AgentRunRequest,
) error {
	desiredState := &api.DesiredState{}
	for _, req := range serverRequests {
		mcpServer, err := r.registryTranslator.TranslateMCPServer(context.TODO(), req)
		if err != nil {
			return fmt.Errorf("translate mcp server %s: %w", req.RegistryServer.Name, err)
		}
		desiredState.MCPServers = append(desiredState.MCPServers, mcpServer)
	}

	for _, req := range agentRequests {
		agent, err := r.registryTranslator.TranslateAgent(context.TODO(), req)
		if err != nil {
			return fmt.Errorf("translate agent %s: %w", req.RegistryAgent.Name, err)
		}
		desiredState.Agents = append(desiredState.Agents, agent)

		serversForConfig := pythonServersFromServerRunRequests(req.ResolvedMCPServers)
		if err := common.RefreshMCPConfig(
			&common.MCPConfigTarget{
				BaseDir:   r.runtimeDir,
				AgentName: req.RegistryAgent.Name,
				Version:   req.RegistryAgent.Version,
			},
			serversForConfig,
			r.verbose,
		); err != nil {
			return fmt.Errorf("failed to refresh resolved MCP server config for agent %s: %w", req.RegistryAgent.Name, err)
		}
	}

	runtimeCfg, err := r.runtimeTranslator.TranslateRuntimeConfig(ctx, desiredState)
	if err != nil {
		return fmt.Errorf("translate runtime config: %w", err)
	}

	if r.verbose {
		fmt.Printf("desired state: agents=%d MCP servers=%d\n", len(desiredState.Agents), len(desiredState.MCPServers))
	}

	return r.ensureRuntime(ctx, runtimeCfg)
}

func (r *agentRegistryRuntime) ensureRuntime(
	ctx context.Context,
	cfg *api.AIRuntimeConfig,
) error {
	switch cfg.Type {
	case api.RuntimeConfigTypeLocal:
		return r.ensureLocalRuntime(ctx, cfg.Local)
	case api.RuntimeConfigTypeKubernetes:
		return r.ensureKubernetesRuntime(ctx, cfg.Kubernetes)
	default:
		return fmt.Errorf("unsupported runtime config type: %v", cfg.Type)
	}
}

func (r *agentRegistryRuntime) ensureLocalRuntime(
	ctx context.Context,
	cfg *api.LocalRuntimeConfig,
) error {
	// step 1: ensure the root runtime dir exists
	if err := os.MkdirAll(r.runtimeDir, 0755); err != nil {
		return fmt.Errorf("failed to create runtime directory: %w", err)
	}
	// step 2: write the docker compose yaml to the dir
	dockerComposeYaml, err := cfg.DockerCompose.MarshalYAML()
	if err != nil {
		return fmt.Errorf("failed to marshal docker compose yaml: %w", err)
	}
	if r.verbose {
		fmt.Printf("Docker Compose YAML:\n%s\n", string(dockerComposeYaml))
	}
	if err := os.WriteFile(filepath.Join(r.runtimeDir, "docker-compose.yaml"), dockerComposeYaml, 0644); err != nil {
		return fmt.Errorf("failed to write docker compose yaml: %w", err)
	}
	// step 3: write the agentconfig yaml to the dir
	agentGatewayYaml, err := yaml.Marshal(cfg.AgentGateway)
	if err != nil {
		return fmt.Errorf("failed to marshal agent config yaml: %w", err)
	}
	if err := os.WriteFile(filepath.Join(r.runtimeDir, "agent-gateway.yaml"), agentGatewayYaml, 0644); err != nil {
		return fmt.Errorf("failed to write agent config yaml: %w", err)
	}
	if r.verbose {
		fmt.Printf("Agent Gateway YAML:\n%s\n", string(agentGatewayYaml))
	}
	// step 4: start docker compose with -d --remove-orphans --force-recreate
	// Using --force-recreate ensures all containers are recreated even if config hasn't changed
	cmd := exec.CommandContext(ctx, "docker", "compose", "up", "-d", "--remove-orphans", "--force-recreate")
	cmd.Dir = r.runtimeDir
	if r.verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	} else {
		cmd.Stdout = nil
		cmd.Stderr = nil
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to start docker compose: %w", err)
	}
	return nil
}

func (r *agentRegistryRuntime) ensureKubernetesRuntime(
	ctx context.Context,
	cfg *api.KubernetesRuntimeConfig,
) error {
	if cfg == nil || (len(cfg.Agents) == 0 && len(cfg.RemoteMCPServers) == 0 && len(cfg.MCPServers) == 0) {
		return nil
	}

	c, err := newClient(r.verbose)
	if err != nil {
		return err
	}

	for _, agent := range cfg.Agents {
		if agent.Namespace == "" {
			agent.Namespace = kagent.DefaultNamespace
		}
		if err := applyResource(ctx, c, agent, r.verbose); err != nil {
			return fmt.Errorf("agent %s: %w", agent.Name, err)
		}
	}

	for _, remoteMCP := range cfg.RemoteMCPServers {
		if remoteMCP.Namespace == "" {
			remoteMCP.Namespace = kagent.DefaultNamespace
		}
		if err := applyResource(ctx, c, remoteMCP, r.verbose); err != nil {
			return fmt.Errorf("remote MCP server %s: %w", remoteMCP.Name, err)
		}
	}

	for _, mcpServer := range cfg.MCPServers {
		if mcpServer.Namespace == "" {
			mcpServer.Namespace = kagent.DefaultNamespace
		}
		if err := applyResource(ctx, c, mcpServer, r.verbose); err != nil {
			return fmt.Errorf("MCP server %s: %w", mcpServer.Name, err)
		}
	}

	return nil
}

// DeleteKubernetesAgent deletes a kagent Agent CR by name/version.
func DeleteKubernetesAgent(ctx context.Context, name, version, namespace string, verbose bool) error {
	if namespace == "" {
		namespace = kagent.DefaultNamespace
	}

	c, err := newClient(verbose)
	if err != nil {
		return err
	}

	agent := &v1alpha2.Agent{}
	agent.Name = kagent.AgentResourceName(name, version)
	agent.Namespace = namespace

	if err := deleteResource(ctx, c, agent); err != nil {
		return fmt.Errorf("failed to delete agent %s: %w", agent.Name, err)
	}
	return nil
}

// DeleteKubernetesRemoteMCPServer deletes a kagent RemoteMCPServer CR by name.
func DeleteKubernetesRemoteMCPServer(ctx context.Context, name, namespace string, verbose bool) error {
	if namespace == "" {
		namespace = kagent.DefaultNamespace
	}

	c, err := newClient(verbose)
	if err != nil {
		return err
	}

	remoteMCP := &v1alpha2.RemoteMCPServer{}
	remoteMCP.Name = kagent.RemoteMCPResourceName(name)
	remoteMCP.Namespace = namespace

	if err := deleteResource(ctx, c, remoteMCP); err != nil {
		return fmt.Errorf("failed to delete remote MCP server %s: %w", remoteMCP.Name, err)
	}
	return nil
}

// DeleteKubernetesMCPServer deletes a kagent MCPServer CR by name.
func DeleteKubernetesMCPServer(ctx context.Context, name, namespace string, verbose bool) error {
	if namespace == "" {
		namespace = kagent.DefaultNamespace
	}

	c, err := newClient(verbose)
	if err != nil {
		return err
	}

	mcpServer := &kmcpv1alpha1.MCPServer{}
	mcpServer.Name = kagent.MCPServerResourceName(name)
	mcpServer.Namespace = namespace

	if err := deleteResource(ctx, c, mcpServer); err != nil {
		return fmt.Errorf("failed to delete MCP server %s: %w", mcpServer.Name, err)
	}
	return nil
}

// pythonServersFromServerRunRequests converts server run requests into Python MCP server structs.
func pythonServersFromServerRunRequests(requests []*registry.MCPServerRunRequest) []common.PythonMCPServer {
	if len(requests) == 0 {
		return nil
	}

	var mcpServers []common.PythonMCPServer
	for _, serverReq := range requests {
		server := serverReq.RegistryServer
		// Skip servers with no remotes or packages
		if len(server.Remotes) == 0 && len(server.Packages) == 0 {
			continue
		}

		pythonServer := common.PythonMCPServer{
			Name: server.Name,
		}

		useRemote := len(server.Remotes) > 0 && (serverReq.PreferRemote || len(server.Packages) == 0)
		if useRemote {
			remote := server.Remotes[0]
			pythonServer.Type = "remote"
			pythonServer.URL = remote.URL

			if len(remote.Headers) > 0 || len(serverReq.HeaderValues) > 0 {
				headers := make(map[string]string)
				for _, h := range remote.Headers {
					headers[h.Name] = h.Value
				}
				maps.Copy(headers, serverReq.HeaderValues)
				if len(headers) > 0 {
					pythonServer.Headers = headers
				}
			}
		} else {
			pythonServer.Type = "command"
			// For command type, Python derives URL as http://{server_name}:3000/mcp
		}

		mcpServers = append(mcpServers, pythonServer)
	}

	return mcpServers
}
