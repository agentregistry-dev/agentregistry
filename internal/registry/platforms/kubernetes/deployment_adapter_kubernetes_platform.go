package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/cli/common/gitutil"
	platformtypes "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/types"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	v1alpha2 "github.com/kagent-dev/kagent/go/api/v1alpha2"
	kmcpv1alpha1 "github.com/kagent-dev/kmcp/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

const (
	defaultKubernetesProviderID         = "kubernetes-default"
	kubernetesManagedLabelKey           = "aregistry.ai/managed"
	kubernetesDeploymentIDLabelKey      = "aregistry.ai/deployment-id"
	kubernetesDeploymentIDAnnotationKey = "aregistry.ai/deployment-id"
	kubernetesFieldManager              = "agentregistry"
	maxKubernetesNameLength             = 63
	maxDeploymentSuffixLength           = 16
)

var (
	kubernetesScheme               = k8sruntime.NewScheme()
	kubernetesGetAmbientRESTConfig = config.GetConfig
	kubernetesNewClientForConfig   = func(restConfig *rest.Config) (client.Client, error) {
		return client.New(restConfig, client.Options{Scheme: kubernetesScheme})
	}
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(kubernetesScheme))
	utilruntime.Must(v1alpha2.AddToScheme(kubernetesScheme))
	utilruntime.Must(kmcpv1alpha1.AddToScheme(kubernetesScheme))
}

func kubernetesDefaultNamespace() string {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, &clientcmd.ConfigOverrides{})
	ns, _, err := kubeConfig.Namespace()
	if err != nil || ns == "" {
		return "default"
	}
	return ns
}

func kubernetesProviderConfig(provider *models.Provider) (*models.KubernetesProviderConfig, error) {
	if provider == nil || len(provider.Config) == 0 {
		return &models.KubernetesProviderConfig{}, nil
	}

	cfg := &models.KubernetesProviderConfig{}
	if err := models.JSONObject(provider.Config).UnmarshalInto(cfg); err != nil {
		return nil, fmt.Errorf("decode kubernetes provider config for %s: %w", provider.ID, err)
	}
	return cfg, nil
}

func kubernetesRESTConfig(provider *models.Provider) (*rest.Config, error) {
	providerCfg, err := kubernetesProviderConfig(provider)
	if err != nil {
		return nil, err
	}

	if kubeconfig := strings.TrimSpace(providerCfg.Kubeconfig); kubeconfig != "" {
		return kubernetesRESTConfigFromInlineKubeconfig(provider, providerCfg, kubeconfig)
	}

	if kubeconfigPath := strings.TrimSpace(providerCfg.KubeconfigPath); kubeconfigPath != "" || strings.TrimSpace(providerCfg.Context) != "" {
		return kubernetesRESTConfigFromPath(providerCfg, kubeconfigPath)
	}

	restConfig, err := kubernetesGetAmbientRESTConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get kubernetes config: %w", err)
	}
	return restConfig, nil
}

func kubernetesRESTConfigFromInlineKubeconfig(
	provider *models.Provider,
	providerCfg *models.KubernetesProviderConfig,
	kubeconfig string,
) (*rest.Config, error) {
	clientCfg, err := clientcmd.NewClientConfigFromBytes([]byte(kubeconfig))
	if err != nil {
		return nil, fmt.Errorf("load kubernetes provider kubeconfig for %s: %w", provider.ID, err)
	}
	rawConfig, err := clientCfg.RawConfig()
	if err != nil {
		return nil, fmt.Errorf("read kubernetes provider kubeconfig for %s: %w", provider.ID, err)
	}
	return clientcmd.NewDefaultClientConfig(rawConfig, kubernetesConfigOverrides(providerCfg)).ClientConfig()
}

func kubernetesRESTConfigFromPath(providerCfg *models.KubernetesProviderConfig, kubeconfigPath string) (*rest.Config, error) {
	loadingRules := &clientcmd.ClientConfigLoadingRules{}
	if kubeconfigPath != "" {
		loadingRules.ExplicitPath = kubeconfigPath
	}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, kubernetesConfigOverrides(providerCfg)).ClientConfig()
}

func kubernetesConfigOverrides(providerCfg *models.KubernetesProviderConfig) *clientcmd.ConfigOverrides {
	overrides := &clientcmd.ConfigOverrides{}
	if providerCfg == nil {
		return overrides
	}
	if contextName := strings.TrimSpace(providerCfg.Context); contextName != "" {
		overrides.CurrentContext = contextName
	}
	return overrides
}

func kubernetesGetClient(provider *models.Provider) (client.Client, error) {
	restConfig, err := kubernetesRESTConfig(provider)
	if err != nil {
		return nil, err
	}

	c, err := kubernetesNewClientForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}
	return c, nil
}

func kubernetesProviderNamespace(provider *models.Provider) string {
	providerCfg, err := kubernetesProviderConfig(provider)
	if err != nil || providerCfg == nil {
		return ""
	}
	return strings.TrimSpace(providerCfg.Namespace)
}

func kubernetesApplyPlatformConfig(ctx context.Context, provider *models.Provider, cfg *platformtypes.KubernetesPlatformConfig, verbose bool) error {
	if cfg == nil || (len(cfg.Agents) == 0 && len(cfg.RemoteMCPServers) == 0 && len(cfg.MCPServers) == 0 && len(cfg.ConfigMaps) == 0) {
		return nil
	}
	c, err := kubernetesGetClient(provider)
	if err != nil {
		return err
	}

	for _, configMap := range cfg.ConfigMaps {
		kubernetesEnsureNamespace(configMap)
		if err := kubernetesApplyResource(ctx, c, configMap, verbose); err != nil {
			return fmt.Errorf("ConfigMap %s: %w", configMap.Name, err)
		}
	}
	for _, agent := range cfg.Agents {
		kubernetesEnsureNamespace(agent)
		if err := kubernetesApplyResource(ctx, c, agent, verbose); err != nil {
			return fmt.Errorf("agent %s: %w", agent.Name, err)
		}
	}
	for _, remoteMCP := range cfg.RemoteMCPServers {
		kubernetesEnsureNamespace(remoteMCP)
		if err := kubernetesApplyResource(ctx, c, remoteMCP, verbose); err != nil {
			return fmt.Errorf("remote MCP server %s: %w", remoteMCP.Name, err)
		}
	}
	for _, mcpServer := range cfg.MCPServers {
		kubernetesEnsureNamespace(mcpServer)
		if err := kubernetesApplyResource(ctx, c, mcpServer, verbose); err != nil {
			return fmt.Errorf("MCP server %s: %w", mcpServer.Name, err)
		}
	}
	return nil
}

func kubernetesDeleteResourcesByDeploymentID(ctx context.Context, provider *models.Provider, deploymentID, resourceType, namespace string) error {
	if deploymentID == "" {
		return fmt.Errorf("deployment id is required")
	}
	c, err := kubernetesGetClient(provider)
	if err != nil {
		return err
	}
	switch resourceType {
	case "agent":
		return kubernetesDeleteAgentResourcesByDeploymentID(ctx, c, deploymentID, namespace)
	case "mcp":
		return kubernetesDeleteMCPResourcesByDeploymentID(ctx, c, deploymentID, namespace)
	default:
		return nil
	}
}

func kubernetesListAgents(ctx context.Context, provider *models.Provider, namespace string) ([]*v1alpha2.Agent, error) {
	c, err := kubernetesGetClient(provider)
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

func kubernetesListMCPServers(ctx context.Context, provider *models.Provider, namespace string) ([]*kmcpv1alpha1.MCPServer, error) {
	c, err := kubernetesGetClient(provider)
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

func kubernetesListRemoteMCPServers(ctx context.Context, provider *models.Provider, namespace string) ([]*v1alpha2.RemoteMCPServer, error) {
	c, err := kubernetesGetClient(provider)
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

func kubernetesTranslatePlatformConfig(ctx context.Context, desired *platformtypes.DesiredState) (*platformtypes.KubernetesPlatformConfig, error) {
	_ = ctx

	agents := make([]*v1alpha2.Agent, 0, len(desired.Agents))
	configMaps := make([]*corev1.ConfigMap, 0)
	for _, agent := range desired.Agents {
		resource, err := kubernetesTranslateAgent(agent)
		if err != nil {
			return nil, err
		}
		agents = append(agents, resource)

		if len(agent.ResolvedMCPServers) > 0 {
			configMap, err := kubernetesTranslateAgentConfigMap(agent)
			if err != nil {
				return nil, fmt.Errorf("failed to create ConfigMap for agent %s: %w", agent.Name, err)
			}
			configMaps = append(configMaps, configMap)
		}
	}

	remoteMCPs := make([]*v1alpha2.RemoteMCPServer, 0)
	mcpServers := make([]*kmcpv1alpha1.MCPServer, 0)
	for _, server := range desired.MCPServers {
		switch server.MCPServerType {
		case platformtypes.MCPServerTypeRemote:
			if server.Remote == nil {
				continue
			}
			resource, err := kubernetesTranslateRemoteMCPServer(server)
			if err != nil {
				return nil, err
			}
			remoteMCPs = append(remoteMCPs, resource)
		case platformtypes.MCPServerTypeLocal:
			if server.Local == nil {
				continue
			}
			resource, err := kubernetesTranslateLocalMCPServer(server)
			if err != nil {
				return nil, err
			}
			mcpServers = append(mcpServers, resource)
		}
	}

	return &platformtypes.KubernetesPlatformConfig{
		Agents:           agents,
		RemoteMCPServers: remoteMCPs,
		MCPServers:       mcpServers,
		ConfigMaps:       configMaps,
	}, nil
}

func kubernetesTranslateAgent(agent *platformtypes.Agent) (*v1alpha2.Agent, error) {
	if agent.Deployment.Image == "" {
		return nil, fmt.Errorf("image must be specified for Agent %s", agent.Name)
	}

	namespace := agent.Deployment.Env["KAGENT_NAMESPACE"]

	envVars := make([]corev1.EnvVar, 0, len(agent.Deployment.Env))
	if len(agent.Deployment.Env) > 0 {
		keys := make([]string, 0, len(agent.Deployment.Env))
		for key := range agent.Deployment.Env {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, key := range keys {
			envVars = append(envVars, corev1.EnvVar{Name: key, Value: agent.Deployment.Env[key]})
		}
	}

	sharedSpec := v1alpha2.SharedDeploymentSpec{Env: envVars}
	if len(agent.ResolvedMCPServers) > 0 {
		configMapName := kubernetesAgentConfigMapName(agent.Name, agent.Version, agent.DeploymentID)
		volumeName := "mcp-config"
		sharedSpec.Volumes = []corev1.Volume{{
			Name: volumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: configMapName},
					Items:                []corev1.KeyToPath{{Key: "mcp-servers.json", Path: "mcp-servers.json"}},
				},
			},
		}}
		sharedSpec.VolumeMounts = []corev1.VolumeMount{{
			Name:      volumeName,
			MountPath: "/config",
			ReadOnly:  true,
		}}
	}

	agentSpec := v1alpha2.AgentSpec{
		Description: agent.Name,
		Type:        v1alpha2.AgentType_BYO,
		BYO: &v1alpha2.BYOAgentSpec{
			Deployment: &v1alpha2.ByoDeploymentSpec{
				Image:                agent.Deployment.Image,
				SharedDeploymentSpec: sharedSpec,
			},
		},
	}
	if len(agent.Skills) > 0 {
		skills, err := kubernetesTranslateSkillsForAgent(agent.Skills)
		if err != nil {
			return nil, fmt.Errorf("translate skills for agent %s: %w", agent.Name, err)
		}
		agentSpec.Skills = skills
	}

	return &v1alpha2.Agent{
		TypeMeta: metav1.TypeMeta{APIVersion: "kagent.dev/v1alpha2", Kind: "Agent"},
		ObjectMeta: metav1.ObjectMeta{
			Name:        kubernetesAgentResourceName(agent.Name, agent.Version, agent.DeploymentID),
			Namespace:   namespace,
			Labels:      kubernetesDeploymentManagedLabels(agent.DeploymentID),
			Annotations: kubernetesDeploymentManagedAnnotations(agent.DeploymentID),
		},
		Spec: agentSpec,
	}, nil
}

func kubernetesTranslateSkillsForAgent(skills []platformtypes.AgentSkillRef) (*v1alpha2.SkillForAgent, error) {
	if len(skills) == 0 {
		return nil, nil
	}

	seenRefs := make(map[string]bool)
	seenGitNames := make(map[string]bool)
	var refs []string
	var gitRefs []v1alpha2.GitRepo

	for _, skill := range skills {
		switch {
		case skill.Image != "":
			if seenRefs[skill.Image] {
				return nil, fmt.Errorf("duplicate skill image ref %q", skill.Image)
			}
			seenRefs[skill.Image] = true
			refs = append(refs, skill.Image)
		case skill.RepoURL != "":
			gr, err := kubernetesBuildGitRepo(skill)
			if err != nil {
				return nil, err
			}
			if seenGitNames[gr.Name] {
				return nil, fmt.Errorf("duplicate skill git name %q (from repo %q)", gr.Name, skill.RepoURL)
			}
			seenGitNames[gr.Name] = true
			gitRefs = append(gitRefs, gr)
		}
	}

	if len(refs) == 0 && len(gitRefs) == 0 {
		return nil, nil
	}

	result := &v1alpha2.SkillForAgent{}
	if len(refs) > 0 {
		result.Refs = refs
	}
	if len(gitRefs) > 0 {
		result.GitRefs = gitRefs
	}
	return result, nil
}

func kubernetesBuildGitRepo(skill platformtypes.AgentSkillRef) (v1alpha2.GitRepo, error) {
	if skill.Name == "" {
		return v1alpha2.GitRepo{}, fmt.Errorf("skill name is required for git-based skill (repo %q)", skill.RepoURL)
	}

	cloneURL, ref, path, err := gitutil.ParseGitHubURL(skill.RepoURL)
	if err != nil {
		return v1alpha2.GitRepo{}, fmt.Errorf("parse skill repo URL %q: %w", skill.RepoURL, err)
	}

	effectivePath := skill.Path
	if effectivePath == "" {
		effectivePath = path
	}

	gr := v1alpha2.GitRepo{URL: cloneURL, Name: skill.Name}
	if skill.Ref != "" {
		gr.Ref = skill.Ref
	} else if ref != "" {
		gr.Ref = ref
	}
	if effectivePath != "" {
		gr.Path = effectivePath
	}
	return gr, nil
}

func kubernetesTranslateRemoteMCPServer(server *platformtypes.MCPServer) (*v1alpha2.RemoteMCPServer, error) {
	if server.Remote == nil {
		return nil, fmt.Errorf("remote MCP server config missing for %s", server.Name)
	}

	url := kubernetesBuildRemoteMCPURL(server.Remote.Host, server.Remote.Port, server.Remote.Path)
	return &v1alpha2.RemoteMCPServer{
		TypeMeta: metav1.TypeMeta{APIVersion: "kagent.dev/v1alpha2", Kind: "RemoteMCPServer"},
		ObjectMeta: metav1.ObjectMeta{
			Name:        kubernetesRemoteMCPResourceName(server.Name, server.DeploymentID),
			Namespace:   server.Namespace,
			Labels:      kubernetesDeploymentManagedLabels(server.DeploymentID),
			Annotations: kubernetesDeploymentManagedAnnotations(server.DeploymentID),
		},
		Spec: v1alpha2.RemoteMCPServerSpec{
			Description: server.Name,
			Protocol:    v1alpha2.RemoteMCPServerProtocolStreamableHttp,
			URL:         url,
		},
	}, nil
}

func kubernetesTranslateLocalMCPServer(server *platformtypes.MCPServer) (*kmcpv1alpha1.MCPServer, error) {
	if server.Local == nil {
		return nil, fmt.Errorf("local MCP server config missing for %s", server.Name)
	}
	if server.Local.TransportType == platformtypes.TransportTypeHTTP && server.Local.HTTP == nil {
		return nil, fmt.Errorf("HTTP transport config missing for %s", server.Name)
	}

	namespace := server.Namespace
	if namespace == "" {
		namespace = server.Local.Deployment.Env["KAGENT_NAMESPACE"]
	}
	deployment := kmcpv1alpha1.MCPServerDeployment{
		Image: server.Local.Deployment.Image,
		Cmd:   server.Local.Deployment.Cmd,
		Args:  server.Local.Deployment.Args,
		Env:   server.Local.Deployment.Env,
	}

	spec := kmcpv1alpha1.MCPServerSpec{Deployment: deployment}
	switch server.Local.TransportType {
	case platformtypes.TransportTypeHTTP:
		spec.TransportType = kmcpv1alpha1.TransportType("http")
		spec.HTTPTransport = &kmcpv1alpha1.HTTPTransport{
			TargetPort: server.Local.HTTP.Port,
			TargetPath: server.Local.HTTP.Path,
		}
		if server.Local.HTTP.Port > 0 {
			spec.Deployment.Port = uint16(server.Local.HTTP.Port)
		}
	case platformtypes.TransportTypeStdio:
		spec.TransportType = kmcpv1alpha1.TransportType("stdio")
		spec.StdioTransport = &kmcpv1alpha1.StdioTransport{}
	default:
		return nil, fmt.Errorf("unsupported MCP transport type %q for %s", server.Local.TransportType, server.Name)
	}

	return &kmcpv1alpha1.MCPServer{
		TypeMeta: metav1.TypeMeta{APIVersion: "kagent.dev/v1alpha1", Kind: "MCPServer"},
		ObjectMeta: metav1.ObjectMeta{
			Name:        kubernetesMCPServerResourceName(server.Name, server.DeploymentID),
			Namespace:   namespace,
			Labels:      kubernetesDeploymentManagedLabels(server.DeploymentID),
			Annotations: kubernetesDeploymentManagedAnnotations(server.DeploymentID),
		},
		Spec: spec,
	}, nil
}

func kubernetesTranslateAgentConfigMap(agent *platformtypes.Agent) (*corev1.ConfigMap, error) {
	namespace := agent.Deployment.Env["KAGENT_NAMESPACE"]
	serversJSON, err := json.MarshalIndent(agent.ResolvedMCPServers, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal MCP servers config: %w", err)
	}

	configMapName := kubernetesAgentConfigMapName(agent.Name, agent.Version, agent.DeploymentID)
	labels := map[string]string{
		"app.kubernetes.io/managed-by": "agentregistry",
		"app.kubernetes.io/component":  "agent-config",
		"agentregistry.dev/agent":      sanitizeKubernetesName(agent.Name),
	}
	maps.Copy(labels, kubernetesDeploymentManagedLabels(agent.DeploymentID))

	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
		ObjectMeta: metav1.ObjectMeta{
			Name:        configMapName,
			Namespace:   namespace,
			Labels:      labels,
			Annotations: kubernetesDeploymentManagedAnnotations(agent.DeploymentID),
		},
		Data: map[string]string{"mcp-servers.json": string(serversJSON)},
	}, nil
}

func kubernetesBuildRemoteMCPURL(host string, port uint32, path string) string {
	host = strings.TrimSpace(host)
	if path == "" {
		path = "/"
	}
	if path[0] != '/' {
		path = "/" + path
	}
	if port == 0 {
		return fmt.Sprintf("http://%s%s", host, path)
	}
	return fmt.Sprintf("http://%s:%d%s", host, port, path)
}

func kubernetesAgentConfigMapName(name, version, deploymentID string) string {
	base := fmt.Sprintf("%s-mcp-config", name)
	if version != "" {
		base = fmt.Sprintf("%s-%s-mcp-config", name, version)
	}
	return kubernetesDeploymentScopedName(base, deploymentID)
}

func kubernetesAgentResourceName(name, version, deploymentID string) string {
	base := name
	if version != "" {
		base = fmt.Sprintf("%s-%s", name, version)
	}
	return kubernetesDeploymentScopedName(base, deploymentID)
}

func kubernetesRemoteMCPResourceName(name, deploymentID string) string {
	return kubernetesDeploymentScopedName(name, deploymentID)
}

func kubernetesMCPServerResourceName(name, deploymentID string) string {
	return kubernetesDeploymentScopedName(name, deploymentID)
}

func kubernetesDeploymentManagedLabels(deploymentID string) map[string]string {
	labels := map[string]string{kubernetesManagedLabelKey: "true"}
	if deploymentID != "" {
		labels[kubernetesDeploymentIDLabelKey] = deploymentID
	}
	return labels
}

func kubernetesDeploymentManagedAnnotations(deploymentID string) map[string]string {
	if deploymentID == "" {
		return nil
	}
	return map[string]string{kubernetesDeploymentIDAnnotationKey: deploymentID}
}

func kubernetesDeploymentScopedName(base, deploymentID string) string {
	sanitizedBase := sanitizeKubernetesName(base)
	suffix := kubernetesDeploymentIDSuffix(deploymentID)
	if suffix == "" {
		return truncateKubernetesName(sanitizedBase)
	}
	maxBaseLen := maxKubernetesNameLength - len(suffix) - 1
	if maxBaseLen < 1 {
		return truncateKubernetesName(suffix)
	}
	truncatedBase := truncateKubernetesNamePart(sanitizedBase, maxBaseLen)
	if truncatedBase == "" {
		return truncateKubernetesName(suffix)
	}
	return truncatedBase + "-" + suffix
}

func kubernetesDeploymentIDSuffix(deploymentID string) string {
	deploymentID = strings.TrimSpace(deploymentID)
	if deploymentID == "" {
		return ""
	}
	id := sanitizeKubernetesName(deploymentID)
	if id == "" {
		return ""
	}
	if len(id) == 36 && strings.Count(id, "-") == 4 {
		if idx := strings.IndexByte(id, '-'); idx > 0 {
			id = id[:idx]
		}
	}
	return truncateKubernetesNamePart(id, maxDeploymentSuffixLength)
}

func truncateKubernetesName(value string) string {
	trimmed := truncateKubernetesNamePart(value, maxKubernetesNameLength)
	if trimmed == "" {
		return "agent"
	}
	return trimmed
}

func truncateKubernetesNamePart(value string, maxLen int) string {
	value = strings.Trim(value, "-")
	if maxLen <= 0 || value == "" {
		return ""
	}
	if len(value) <= maxLen {
		return value
	}
	return strings.Trim(value[:maxLen], "-")
}

func sanitizeKubernetesName(value string) string {
	value = strings.ToLower(value)
	var b strings.Builder
	prevDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
			continue
		}
		if !prevDash {
			b.WriteRune('-')
			prevDash = true
		}
	}
	result := strings.Trim(b.String(), "-")
	if result == "" {
		return "agent"
	}
	return truncateKubernetesName(result)
}

func kubernetesApplyResource(ctx context.Context, c client.Client, obj client.Object, verbose bool) error {
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

	if err := c.Apply(ctx, applyCfg, client.FieldOwner(kubernetesFieldManager), client.ForceOwnership); err != nil {
		return fmt.Errorf("failed to apply %s %s: %w", kind, obj.GetName(), err)
	}

	if verbose {
		fmt.Printf("Applied %s %s\n", kind, obj.GetName())
	}
	return nil
}

func kubernetesDeleteResource(ctx context.Context, c client.Client, obj client.Object) error {
	if err := c.Delete(ctx, obj); client.IgnoreNotFound(err) != nil {
		return err
	}
	return nil
}

func kubernetesEnsureNamespace(obj client.Object) {
	if obj.GetNamespace() == "" {
		obj.SetNamespace(kubernetesDefaultNamespace())
	}
}

func kubernetesDeploymentSelectorOpts(deploymentID, namespace string) []client.ListOption {
	opts := []client.ListOption{
		client.MatchingLabels{kubernetesDeploymentIDLabelKey: deploymentID},
	}
	if namespace != "" {
		opts = append(opts, client.InNamespace(namespace))
	}
	return opts
}

func kubernetesDeleteAgentResourcesByDeploymentID(ctx context.Context, c client.Client, deploymentID, namespace string) error {
	opts := kubernetesDeploymentSelectorOpts(deploymentID, namespace)
	agentList := &v1alpha2.AgentList{}
	if err := c.List(ctx, agentList, opts...); err != nil {
		return fmt.Errorf("failed to list agents by deployment id %s: %w", deploymentID, err)
	}
	for i := range agentList.Items {
		if err := kubernetesDeleteResource(ctx, c, &agentList.Items[i]); err != nil {
			return fmt.Errorf("failed to delete agent %s: %w", agentList.Items[i].Name, err)
		}
	}

	configMapList := &corev1.ConfigMapList{}
	if err := c.List(ctx, configMapList, opts...); err != nil {
		return fmt.Errorf("failed to list configmaps by deployment id %s: %w", deploymentID, err)
	}
	for i := range configMapList.Items {
		if err := kubernetesDeleteResource(ctx, c, &configMapList.Items[i]); err != nil {
			return fmt.Errorf("failed to delete configmap %s: %w", configMapList.Items[i].Name, err)
		}
	}

	remoteMCPList := &v1alpha2.RemoteMCPServerList{}
	if err := c.List(ctx, remoteMCPList, opts...); err != nil {
		return fmt.Errorf("failed to list remote mcp servers by deployment id %s: %w", deploymentID, err)
	}
	for i := range remoteMCPList.Items {
		if err := kubernetesDeleteResource(ctx, c, &remoteMCPList.Items[i]); err != nil {
			return fmt.Errorf("failed to delete remote mcp server %s: %w", remoteMCPList.Items[i].Name, err)
		}
	}

	mcpList := &kmcpv1alpha1.MCPServerList{}
	if err := c.List(ctx, mcpList, opts...); err != nil {
		return fmt.Errorf("failed to list mcp servers by deployment id %s: %w", deploymentID, err)
	}
	for i := range mcpList.Items {
		if err := kubernetesDeleteResource(ctx, c, &mcpList.Items[i]); err != nil {
			return fmt.Errorf("failed to delete mcp server %s: %w", mcpList.Items[i].Name, err)
		}
	}
	return nil
}

func kubernetesDeleteMCPResourcesByDeploymentID(ctx context.Context, c client.Client, deploymentID, namespace string) error {
	opts := kubernetesDeploymentSelectorOpts(deploymentID, namespace)

	mcpList := &kmcpv1alpha1.MCPServerList{}
	if err := c.List(ctx, mcpList, opts...); err != nil {
		return fmt.Errorf("failed to list mcp servers by deployment id %s: %w", deploymentID, err)
	}
	for i := range mcpList.Items {
		if err := kubernetesDeleteResource(ctx, c, &mcpList.Items[i]); err != nil {
			return fmt.Errorf("failed to delete mcp server %s: %w", mcpList.Items[i].Name, err)
		}
	}

	remoteMCPList := &v1alpha2.RemoteMCPServerList{}
	if err := c.List(ctx, remoteMCPList, opts...); err != nil {
		return fmt.Errorf("failed to list remote mcp servers by deployment id %s: %w", deploymentID, err)
	}
	for i := range remoteMCPList.Items {
		if err := kubernetesDeleteResource(ctx, c, &remoteMCPList.Items[i]); err != nil {
			return fmt.Errorf("failed to delete remote mcp server %s: %w", remoteMCPList.Items[i].Name, err)
		}
	}
	return nil
}

func kubernetesDiscoverDeployments(ctx context.Context, provider *models.Provider) ([]*models.Deployment, error) {
	providerID := defaultKubernetesProviderID
	if provider != nil && strings.TrimSpace(provider.ID) != "" {
		providerID = strings.TrimSpace(provider.ID)
	}
	namespace := kubernetesProviderNamespace(provider)

	isManaged := func(labels map[string]string) bool {
		return labels != nil && labels[kubernetesManagedLabelKey] == "true"
	}

	discovered := make([]*models.Deployment, 0)
	appendResource := func(resType, name, resourceNamespace string, labels map[string]string, creation time.Time) {
		if isManaged(labels) {
			return
		}
		resourceType := "agent"
		if resType == "mcpserver" || resType == "remotemcpserver" {
			resourceType = "mcp"
		}
		preferRemote := resType == "remotemcpserver"
		meta, _ := models.UnmarshalFrom(models.KubernetesProviderMetadata{
			IsExternal: true,
			Namespace:  resourceNamespace,
		})
		discovered = append(discovered, &models.Deployment{
			ServerName:       name,
			Version:          "unknown",
			DeployedAt:       creation,
			UpdatedAt:        creation,
			Status:           "deployed",
			Origin:           "discovered",
			ProviderID:       providerID,
			ResourceType:     resourceType,
			PreferRemote:     preferRemote,
			Env:              labels,
			ProviderMetadata: meta,
		})
	}

	agents, err := kubernetesListAgents(ctx, provider, namespace)
	if err != nil {
		log.Printf("Warning: failed to list kubernetes agents for discovery: %v", err)
	} else {
		for _, agent := range agents {
			appendResource("agent", agent.Name, agent.Namespace, agent.Labels, agent.CreationTimestamp.Time)
		}
	}

	mcpServers, err := kubernetesListMCPServers(ctx, provider, namespace)
	if err != nil {
		log.Printf("Warning: failed to list kubernetes MCP servers for discovery: %v", err)
	} else {
		for _, mcp := range mcpServers {
			appendResource("mcpserver", mcp.Name, mcp.Namespace, mcp.Labels, mcp.CreationTimestamp.Time)
		}
	}

	remoteMCPs, err := kubernetesListRemoteMCPServers(ctx, provider, namespace)
	if err != nil {
		log.Printf("Warning: failed to list kubernetes remote MCP servers for discovery: %v", err)
	} else {
		for _, remote := range remoteMCPs {
			appendResource("remotemcpserver", remote.Name, remote.Namespace, remote.Labels, remote.CreationTimestamp.Time)
		}
	}

	return discovered, nil
}
