package runtime

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
	"github.com/agentregistry-dev/agentregistry/internal/runtime/translation/api"
	"github.com/agentregistry-dev/agentregistry/internal/runtime/translation/kagent"
	"github.com/agentregistry-dev/agentregistry/internal/runtime/translation/registry"
	v1alpha2 "github.com/kagent-dev/kagent/go/api/v1alpha2"
	kmcpv1alpha1 "github.com/kagent-dev/kmcp/api/v1alpha1"
	"go.yaml.in/yaml/v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
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

var (
	k8sClient    client.Client
	k8sClientErr error
	clientOnce   sync.Once
)

// controller-runtime client singleton
func GetKubeClient() (client.Client, error) {
	clientOnce.Do(func() {
		var restConfig *rest.Config
		restConfig, k8sClientErr = config.GetConfig()
		if k8sClientErr != nil {
			k8sClientErr = fmt.Errorf("failed to get kubernetes config: %w", k8sClientErr)
			return
		}

		k8sClient, k8sClientErr = client.New(restConfig, client.Options{Scheme: scheme})
		if k8sClientErr != nil {
			k8sClientErr = fmt.Errorf("failed to create kubernetes client: %w", k8sClientErr)
			return
		}
	})

	if k8sClientErr != nil {
		return nil, k8sClientErr
	}
	return k8sClient, nil
}

// applyResource uses server-side apply to create or update a Kubernetes resource.
func applyResource(ctx context.Context, c client.Client, obj client.Object, verbose bool) error {
	kind := obj.GetObjectKind().GroupVersionKind().Kind
	if verbose {
		fmt.Printf("Applying %s %s in namespace %s\n", kind, obj.GetName(), obj.GetNamespace())
	}

	raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return fmt.Errorf("failed to convert %s %s to unstructured: %w", kind, obj.GetName(), err)
	}
	u := &unstructured.Unstructured{Object: raw}
	applyCfg := client.ApplyConfigurationFromUnstructured(u)

	if err := c.Apply(ctx, applyCfg, client.FieldOwner(fieldManager), client.ForceOwnership); err != nil {
		return fmt.Errorf("failed to apply %s %s: %w", kind, obj.GetName(), err)
	}

	if verbose {
		fmt.Printf("Applied %s %s\n", kind, obj.GetName())
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

// DefaultNamespace returns the namespace from the current kubeconfig context,
// falling back to "default" if it cannot be determined.
func DefaultNamespace() string {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, &clientcmd.ConfigOverrides{})
	ns, _, err := kubeConfig.Namespace()
	if err != nil || ns == "" {
		return "default"
	}
	return ns
}

// ensureNamespace sets the namespace on a resource if it is empty.
// Namespaced Kubernetes resources require a non-empty namespace for all operations.
func ensureNamespace(obj client.Object) {
	if obj.GetNamespace() == "" {
		obj.SetNamespace(DefaultNamespace())
	}
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
		if ns := req.EnvValues["KAGENT_NAMESPACE"]; ns != "" && mcpServer.Namespace == "" {
			mcpServer.Namespace = ns
		}
		desiredState.MCPServers = append(desiredState.MCPServers, mcpServer)
	}

	for _, req := range agentRequests {
		agent, err := r.registryTranslator.TranslateAgent(context.TODO(), req)
		if err != nil {
			return fmt.Errorf("translate agent %s: %w", req.RegistryAgent.Name, err)
		}

		// Extract namespace from agent's env (if set) to propagate to MCP servers
		agentNamespace := ""
		if ns, ok := req.EnvValues["KAGENT_NAMESPACE"]; ok && ns != "" {
			agentNamespace = ns
		}

		// Translate and add resolved MCP servers from agent manifest to desired state
		for _, serverReq := range req.ResolvedMCPServers {
			mcpServer, err := r.registryTranslator.TranslateMCPServer(context.TODO(), serverReq)
			if err != nil {
				return fmt.Errorf("translate resolved MCP server %s for agent %s: %w", serverReq.RegistryServer.Name, req.RegistryAgent.Name, err)
			}
			// Propagate namespace from agent to MCP server for co-location
			if agentNamespace != "" {
				mcpServer.Namespace = agentNamespace
			}
			desiredState.MCPServers = append(desiredState.MCPServers, mcpServer)
		}

		// Populate ResolvedMCPServers on the agent for ConfigMap generation
		resolvedConfigs := createResolvedMCPServerConfigs(req.ResolvedMCPServers)
		agent.ResolvedMCPServers = resolvedConfigs

		desiredState.Agents = append(desiredState.Agents, agent)

		// Convert back to PythonMCPServer for local runtime backward compatibility
		var pythonServers []common.PythonMCPServer
		for _, cfg := range resolvedConfigs {
			pythonServers = append(pythonServers, common.PythonMCPServer{
				Name:    cfg.Name,
				Type:    cfg.Type,
				URL:     cfg.URL,
				Headers: cfg.Headers,
			})
		}

		if err := common.RefreshMCPConfig(
			&common.MCPConfigTarget{
				BaseDir:   r.runtimeDir,
				AgentName: req.RegistryAgent.Name,
				Version:   req.RegistryAgent.Version,
			},
			pythonServers,
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
	var stderrBuf bytes.Buffer
	if r.verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)
	} else {
		cmd.Stderr = &stderrBuf
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to start docker compose: %w: %s", err, strings.TrimSpace(stderrBuf.String()))
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

	c, err := GetKubeClient()
	if err != nil {
		return err
	}

	// Apply ConfigMaps first
	for _, configMap := range cfg.ConfigMaps {
		ensureNamespace(configMap)
		if err := applyResource(ctx, c, configMap, r.verbose); err != nil {
			return fmt.Errorf("ConfigMap %s: %w", configMap.Name, err)
		}
	}

	for _, agent := range cfg.Agents {
		ensureNamespace(agent)
		if err := applyResource(ctx, c, agent, r.verbose); err != nil {
			return fmt.Errorf("agent %s: %w", agent.Name, err)
		}
	}

	for _, remoteMCP := range cfg.RemoteMCPServers {
		ensureNamespace(remoteMCP)
		if err := applyResource(ctx, c, remoteMCP, r.verbose); err != nil {
			return fmt.Errorf("remote MCP server %s: %w", remoteMCP.Name, err)
		}
	}

	for _, mcpServer := range cfg.MCPServers {
		ensureNamespace(mcpServer)
		if err := applyResource(ctx, c, mcpServer, r.verbose); err != nil {
			return fmt.Errorf("MCP server %s: %w", mcpServer.Name, err)
		}
	}

	return nil
}

// ListAgents lists all Agent CRs in the given namespace (or all namespaces if empty)
func ListAgents(ctx context.Context, namespace string) ([]*v1alpha2.Agent, error) {
	c, err := GetKubeClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	agentList := &v1alpha2.AgentList{}
	listOpts := []client.ListOption{}
	if namespace != "" {
		listOpts = append(listOpts, client.InNamespace(namespace))
	}

	if err := c.List(ctx, agentList, listOpts...); err != nil {
		return nil, fmt.Errorf("failed to list agents: %w", err)
	}

	agents := make([]*v1alpha2.Agent, 0, len(agentList.Items))
	for i := range agentList.Items {
		agents = append(agents, &agentList.Items[i])
	}
	return agents, nil
}

// ListMCPServers lists all MCPServer CRs in the given namespace (or all namespaces if empty)
func ListMCPServers(ctx context.Context, namespace string) ([]*kmcpv1alpha1.MCPServer, error) {
	c, err := GetKubeClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	mcpList := &kmcpv1alpha1.MCPServerList{}
	listOpts := []client.ListOption{}
	if namespace != "" {
		listOpts = append(listOpts, client.InNamespace(namespace))
	}

	if err := c.List(ctx, mcpList, listOpts...); err != nil {
		return nil, fmt.Errorf("failed to list MCP servers: %w", err)
	}

	servers := make([]*kmcpv1alpha1.MCPServer, 0, len(mcpList.Items))
	for i := range mcpList.Items {
		servers = append(servers, &mcpList.Items[i])
	}
	return servers, nil
}

// ListRemoteMCPServers lists all RemoteMCPServer CRs in the given namespace (or all namespaces if empty)
func ListRemoteMCPServers(ctx context.Context, namespace string) ([]*v1alpha2.RemoteMCPServer, error) {
	c, err := GetKubeClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	remoteMCPList := &v1alpha2.RemoteMCPServerList{}
	listOpts := []client.ListOption{}
	if namespace != "" {
		listOpts = append(listOpts, client.InNamespace(namespace))
	}

	if err := c.List(ctx, remoteMCPList, listOpts...); err != nil {
		return nil, fmt.Errorf("failed to list remote MCP servers: %w", err)
	}

	servers := make([]*v1alpha2.RemoteMCPServer, 0, len(remoteMCPList.Items))
	for i := range remoteMCPList.Items {
		servers = append(servers, &remoteMCPList.Items[i])
	}
	return servers, nil
}

// DeleteKubernetesResourcesByDeploymentID removes runtime resources tagged to one deployment row.
func DeleteKubernetesResourcesByDeploymentID(ctx context.Context, deploymentID, resourceType, namespace string) error {
	if strings.TrimSpace(deploymentID) == "" {
		return fmt.Errorf("deployment id is required")
	}
	c, err := GetKubeClient()
	if err != nil {
		return err
	}

	switch strings.ToLower(strings.TrimSpace(resourceType)) {
	case "agent":
		return deleteKubernetesAgentResourcesByDeploymentID(ctx, c, deploymentID, namespace)
	case "mcp":
		return deleteKubernetesMCPResourcesByDeploymentID(ctx, c, deploymentID, namespace)
	default:
		return nil
	}
}

func deploymentSelectorOpts(deploymentID, namespace string) []client.ListOption {
	opts := []client.ListOption{
		client.MatchingLabels{kagent.DeploymentIDLabelKey: deploymentID},
	}
	if strings.TrimSpace(namespace) != "" {
		opts = append(opts, client.InNamespace(namespace))
	}
	return opts
}

func deleteKubernetesAgentResourcesByDeploymentID(ctx context.Context, c client.Client, deploymentID, namespace string) error {
	opts := deploymentSelectorOpts(deploymentID, namespace)
	var errs []error

	agentList := &v1alpha2.AgentList{}
	if err := c.List(ctx, agentList, opts...); err != nil {
		return fmt.Errorf("failed to list agents by deployment id %s: %w", deploymentID, err)
	}
	for i := range agentList.Items {
		if err := deleteResource(ctx, c, &agentList.Items[i]); err != nil {
			errs = append(errs, fmt.Errorf("failed to delete agent %s: %w", agentList.Items[i].Name, err))
		}
	}

	configMapList := &corev1.ConfigMapList{}
	if err := c.List(ctx, configMapList, opts...); err != nil {
		return fmt.Errorf("failed to list configmaps by deployment id %s: %w", deploymentID, err)
	}
	for i := range configMapList.Items {
		if err := deleteResource(ctx, c, &configMapList.Items[i]); err != nil {
			errs = append(errs, fmt.Errorf("failed to delete configmap %s: %w", configMapList.Items[i].Name, err))
		}
	}

	return errors.Join(errs...)
}

func deleteKubernetesMCPResourcesByDeploymentID(ctx context.Context, c client.Client, deploymentID, namespace string) error {
	opts := deploymentSelectorOpts(deploymentID, namespace)
	var errs []error

	mcpList := &kmcpv1alpha1.MCPServerList{}
	if err := c.List(ctx, mcpList, opts...); err != nil {
		return fmt.Errorf("failed to list mcp servers by deployment id %s: %w", deploymentID, err)
	}
	for i := range mcpList.Items {
		if err := deleteResource(ctx, c, &mcpList.Items[i]); err != nil {
			errs = append(errs, fmt.Errorf("failed to delete mcp server %s: %w", mcpList.Items[i].Name, err))
		}
	}

	remoteMCPList := &v1alpha2.RemoteMCPServerList{}
	if err := c.List(ctx, remoteMCPList, opts...); err != nil {
		return fmt.Errorf("failed to list remote mcp servers by deployment id %s: %w", deploymentID, err)
	}
	for i := range remoteMCPList.Items {
		if err := deleteResource(ctx, c, &remoteMCPList.Items[i]); err != nil {
			errs = append(errs, fmt.Errorf("failed to delete remote mcp server %s: %w", remoteMCPList.Items[i].Name, err))
		}
	}

	return errors.Join(errs...)
}

// createResolvedMCPServerConfigs converts server run requests into API ResolvedMCPServerConfig
func createResolvedMCPServerConfigs(requests []*registry.MCPServerRunRequest) []api.ResolvedMCPServerConfig {
	if len(requests) == 0 {
		return nil
	}

	var configs []api.ResolvedMCPServerConfig
	for _, serverReq := range requests {
		server := serverReq.RegistryServer
		// Skip servers with no remotes or packages
		if len(server.Remotes) == 0 && len(server.Packages) == 0 {
			continue
		}

		// Use user-provided name when available, otherwise fall back to registry name
		effectiveName := serverReq.Name
		if effectiveName == "" {
			effectiveName = server.Name
		}
		config := api.ResolvedMCPServerConfig{
			Name: registry.GenerateInternalNameForDeployment(effectiveName, serverReq.DeploymentID),
		}

		useRemote := len(server.Remotes) > 0 && (serverReq.PreferRemote || len(server.Packages) == 0)
		if useRemote {
			remote := server.Remotes[0]
			config.Type = "remote"
			config.URL = remote.URL

			if len(remote.Headers) > 0 || len(serverReq.HeaderValues) > 0 {
				headers := make(map[string]string)
				for _, h := range remote.Headers {
					headers[h.Name] = h.Value
				}
				maps.Copy(headers, serverReq.HeaderValues)
				if len(headers) > 0 {
					config.Headers = headers
				}
			}
		} else {
			// For command type, URL is derived internally by the client (http://{server_name}:port)
			config.Type = "command"
		}

		configs = append(configs, config)
	}

	return configs
}
