package kubernetes

import (
	"context"
	"fmt"
	"sync"

	api "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/types"
	v1alpha2 "github.com/kagent-dev/kagent/go/api/v1alpha2"
	kmcpv1alpha1 "github.com/kagent-dev/kmcp/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

const fieldManager = "agentregistry"

var (
	scheme = k8sruntime.NewScheme()

	k8sClient    client.Client
	k8sClientErr error
	clientOnce   sync.Once
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha2.AddToScheme(scheme))
	utilruntime.Must(kmcpv1alpha1.AddToScheme(scheme))
}

func GetClient() (client.Client, error) {
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

func DefaultNamespace() string {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, &clientcmd.ConfigOverrides{})
	ns, _, err := kubeConfig.Namespace()
	if err != nil || ns == "" {
		return "default"
	}
	return ns
}

func ApplyPlatformConfig(ctx context.Context, cfg *api.KubernetesPlatformConfig, verbose bool) error {
	if cfg == nil || (len(cfg.Agents) == 0 && len(cfg.RemoteMCPServers) == 0 && len(cfg.MCPServers) == 0 && len(cfg.ConfigMaps) == 0) {
		return nil
	}
	c, err := GetClient()
	if err != nil {
		return err
	}

	for _, configMap := range cfg.ConfigMaps {
		ensureNamespace(configMap)
		if err := applyResource(ctx, c, configMap, verbose); err != nil {
			return fmt.Errorf("ConfigMap %s: %w", configMap.Name, err)
		}
	}
	for _, agent := range cfg.Agents {
		ensureNamespace(agent)
		if err := applyResource(ctx, c, agent, verbose); err != nil {
			return fmt.Errorf("agent %s: %w", agent.Name, err)
		}
	}
	for _, remoteMCP := range cfg.RemoteMCPServers {
		ensureNamespace(remoteMCP)
		if err := applyResource(ctx, c, remoteMCP, verbose); err != nil {
			return fmt.Errorf("remote MCP server %s: %w", remoteMCP.Name, err)
		}
	}
	for _, mcpServer := range cfg.MCPServers {
		ensureNamespace(mcpServer)
		if err := applyResource(ctx, c, mcpServer, verbose); err != nil {
			return fmt.Errorf("MCP server %s: %w", mcpServer.Name, err)
		}
	}
	return nil
}

func ListAgents(ctx context.Context, namespace string) ([]*v1alpha2.Agent, error) {
	c, err := GetClient()
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

func ListMCPServers(ctx context.Context, namespace string) ([]*kmcpv1alpha1.MCPServer, error) {
	c, err := GetClient()
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

func ListRemoteMCPServers(ctx context.Context, namespace string) ([]*v1alpha2.RemoteMCPServer, error) {
	c, err := GetClient()
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

func DeleteResourcesByDeploymentID(ctx context.Context, deploymentID, resourceType, namespace string) error {
	if deploymentID == "" {
		return fmt.Errorf("deployment id is required")
	}
	c, err := GetClient()
	if err != nil {
		return err
	}
	switch resourceType {
	case "agent":
		return deleteAgentResourcesByDeploymentID(ctx, c, deploymentID, namespace)
	case "mcp":
		return deleteMCPResourcesByDeploymentID(ctx, c, deploymentID, namespace)
	default:
		return nil
	}
}

func applyResource(ctx context.Context, c client.Client, obj client.Object, verbose bool) error {
	kind := obj.GetObjectKind().GroupVersionKind().Kind
	if verbose {
		fmt.Printf("Applying %s %s in namespace %s\n", kind, obj.GetName(), obj.GetNamespace())
	}

	raw, err := k8sruntime.DefaultUnstructuredConverter.ToUnstructured(obj)
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

func deleteResource(ctx context.Context, c client.Client, obj client.Object) error {
	if err := c.Delete(ctx, obj); client.IgnoreNotFound(err) != nil {
		return err
	}
	return nil
}

func ensureNamespace(obj client.Object) {
	if obj.GetNamespace() == "" {
		obj.SetNamespace(DefaultNamespace())
	}
}

func deploymentSelectorOpts(deploymentID, namespace string) []client.ListOption {
	opts := []client.ListOption{
		client.MatchingLabels{DeploymentIDLabelKey: deploymentID},
	}
	if namespace != "" {
		opts = append(opts, client.InNamespace(namespace))
	}
	return opts
}

func deleteAgentResourcesByDeploymentID(ctx context.Context, c client.Client, deploymentID, namespace string) error {
	opts := deploymentSelectorOpts(deploymentID, namespace)
	agentList := &v1alpha2.AgentList{}
	if err := c.List(ctx, agentList, opts...); err != nil {
		return fmt.Errorf("failed to list agents by deployment id %s: %w", deploymentID, err)
	}
	for i := range agentList.Items {
		if err := deleteResource(ctx, c, &agentList.Items[i]); err != nil {
			return fmt.Errorf("failed to delete agent %s: %w", agentList.Items[i].Name, err)
		}
	}

	configMapList := &corev1.ConfigMapList{}
	if err := c.List(ctx, configMapList, opts...); err != nil {
		return fmt.Errorf("failed to list configmaps by deployment id %s: %w", deploymentID, err)
	}
	for i := range configMapList.Items {
		if err := deleteResource(ctx, c, &configMapList.Items[i]); err != nil {
			return fmt.Errorf("failed to delete configmap %s: %w", configMapList.Items[i].Name, err)
		}
	}
	return nil
}

func deleteMCPResourcesByDeploymentID(ctx context.Context, c client.Client, deploymentID, namespace string) error {
	opts := deploymentSelectorOpts(deploymentID, namespace)

	mcpList := &kmcpv1alpha1.MCPServerList{}
	if err := c.List(ctx, mcpList, opts...); err != nil {
		return fmt.Errorf("failed to list mcp servers by deployment id %s: %w", deploymentID, err)
	}
	for i := range mcpList.Items {
		if err := deleteResource(ctx, c, &mcpList.Items[i]); err != nil {
			return fmt.Errorf("failed to delete mcp server %s: %w", mcpList.Items[i].Name, err)
		}
	}

	remoteMCPList := &v1alpha2.RemoteMCPServerList{}
	if err := c.List(ctx, remoteMCPList, opts...); err != nil {
		return fmt.Errorf("failed to list remote mcp servers by deployment id %s: %w", deploymentID, err)
	}
	for i := range remoteMCPList.Items {
		if err := deleteResource(ctx, c, &remoteMCPList.Items[i]); err != nil {
			return fmt.Errorf("failed to delete remote mcp server %s: %w", remoteMCPList.Items[i].Name, err)
		}
	}
	return nil
}
