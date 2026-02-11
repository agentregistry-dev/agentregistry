package kagent

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	api "github.com/agentregistry-dev/agentregistry/internal/runtime/translation/api"
	v1alpha2 "github.com/kagent-dev/kagent/go/api/v1alpha2"
	kmcpv1alpha1 "github.com/kagent-dev/kmcp/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type translator struct{}

// NewTranslator returns a Kubernetes runtime translator that renders kagent Agent CRs.
func NewTranslator() api.RuntimeTranslator {
	return &translator{}
}

// TranslateRuntimeConfig translates the desired state into a Kubernetes runtime config supported by Kagent.
// This handles agent, local and remote MCP servers.
func (t *translator) TranslateRuntimeConfig(
	ctx context.Context,
	desired *api.DesiredState,
) (*api.AIRuntimeConfig, error) {
	agents := make([]*v1alpha2.Agent, 0, len(desired.Agents))
	configMaps := make([]*corev1.ConfigMap, 0)

	for _, agent := range desired.Agents {
		resource, err := t.translateAgent(agent)
		if err != nil {
			return nil, err
		}
		agents = append(agents, resource)

		// Generate ConfigMap for agent's resolved MCP server connections
		if len(agent.ResolvedMCPServers) > 0 {
			configMap, err := t.translateAgentConfigMap(agent)
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
		case api.MCPServerTypeRemote:
			if server.Remote == nil {
				continue
			}
			resource, err := t.translateRemoteMCPServer(server)
			if err != nil {
				return nil, err
			}
			remoteMCPs = append(remoteMCPs, resource)
		case api.MCPServerTypeLocal:
			if server.Local == nil {
				continue
			}
			resource, err := t.translateLocalMCPServer(server)
			if err != nil {
				return nil, err
			}
			mcpServers = append(mcpServers, resource)
		}
	}

	return &api.AIRuntimeConfig{
		Type: api.RuntimeConfigTypeKubernetes,
		Kubernetes: &api.KubernetesRuntimeConfig{
			Agents:           agents,
			RemoteMCPServers: remoteMCPs,
			MCPServers:       mcpServers,
			ConfigMaps:       configMaps,
		},
	}, nil
}

// translateAgent translates an Agent into a Kagent Agent CRD
func (t *translator) translateAgent(agent *api.Agent) (*v1alpha2.Agent, error) {
	if agent.Deployment.Image == "" {
		return nil, fmt.Errorf("image must be specified for Agent %s", agent.Name)
	}

	// Use namespace from KAGENT_NAMESPACE env if set; otherwise leave empty
	// and let the runtime layer resolve from kubeconfig context.
	namespace := agent.Deployment.Env["KAGENT_NAMESPACE"]

	envVars := make([]corev1.EnvVar, 0, len(agent.Deployment.Env))
	if len(agent.Deployment.Env) > 0 {
		keys := make([]string, 0, len(agent.Deployment.Env))
		for key := range agent.Deployment.Env {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, key := range keys {
			envVars = append(envVars, corev1.EnvVar{
				Name:  key,
				Value: agent.Deployment.Env[key],
			})
		}
	}

	// Build SharedDeploymentSpec with optional ConfigMap volume mount for resolved MCP servers
	sharedSpec := v1alpha2.SharedDeploymentSpec{
		Env: envVars,
	}

	// If agent has resolved MCP servers, add ConfigMap volume mount
	if len(agent.ResolvedMCPServers) > 0 {
		configMapName := AgentConfigMapName(agent.Name, agent.Version)
		volumeName := "mcp-config"

		sharedSpec.Volumes = []corev1.Volume{{
			Name: volumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: configMapName,
					},
					Items: []corev1.KeyToPath{{
						Key:  "mcp-servers.json",
						Path: "mcp-servers.json",
					}},
				},
			},
		}}

		sharedSpec.VolumeMounts = []corev1.VolumeMount{{
			Name:      volumeName,
			MountPath: "/config",
			ReadOnly:  true,
		}}
	}

	return &v1alpha2.Agent{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "kagent.dev/v1alpha2",
			Kind:       "Agent",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      AgentResourceName(agent.Name, agent.Version),
			Namespace: namespace,
			// Add a label to identify this resource as managed by agentregistry
			Labels: map[string]string{
				"aregistry.ai/managed": "true",
			},
		},
		Spec: v1alpha2.AgentSpec{
			Description: agent.Name,
			Type:        v1alpha2.AgentType_BYO,
			BYO: &v1alpha2.BYOAgentSpec{
				Deployment: &v1alpha2.ByoDeploymentSpec{
					Image:                agent.Deployment.Image,
					SharedDeploymentSpec: sharedSpec,
				},
			},
		},
	}, nil
}

// translateRemoteMCPServer translates a remote MCP server into a Kagent RemoteMCPServer CRD
func (t *translator) translateRemoteMCPServer(server *api.MCPServer) (*v1alpha2.RemoteMCPServer, error) {
	if server.Remote == nil {
		return nil, fmt.Errorf("remote MCP server config missing for %s", server.Name)
	}

	url := buildRemoteMCPURL(server.Remote.Host, server.Remote.Port, server.Remote.Path)
	// Use namespace from MCPServer if set (propagated from agent's deployment config);
	// otherwise leave empty and let the runtime layer resolve from kubeconfig context.
	namespace := server.Namespace

	return &v1alpha2.RemoteMCPServer{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "kagent.dev/v1alpha2",
			Kind:       "RemoteMCPServer",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      RemoteMCPResourceName(server.Name),
			Namespace: namespace,
			Labels: map[string]string{
				"aregistry.ai/managed": "true",
			},
		},
		Spec: v1alpha2.RemoteMCPServerSpec{
			Description: server.Name,
			Protocol:    v1alpha2.RemoteMCPServerProtocolStreamableHttp,
			URL:         url,
		},
	}, nil
}

// translateLocalMCPServer translates a local MCP server into a KMCP MCPServer CRD
func (t *translator) translateLocalMCPServer(server *api.MCPServer) (*kmcpv1alpha1.MCPServer, error) {
	if server.Local == nil {
		return nil, fmt.Errorf("local MCP server config missing for %s", server.Name)
	}
	if server.Local.TransportType == api.TransportTypeHTTP && server.Local.HTTP == nil {
		return nil, fmt.Errorf("HTTP transport config missing for %s", server.Name)
	}

	// Use namespace from MCPServer if set (propagated from agent's deployment config);
	// fall back to KAGENT_NAMESPACE env; otherwise leave empty and let the runtime
	// layer resolve from kubeconfig context.
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
	fmt.Printf("[DEBUG] kagent translateLocalMCPServer: name=%s, image=%s, cmd=%q, args=%v\n",
		server.Name, deployment.Image, deployment.Cmd, deployment.Args)

	spec := kmcpv1alpha1.MCPServerSpec{
		Deployment: deployment,
	}

	switch server.Local.TransportType {
	case api.TransportTypeHTTP:
		spec.TransportType = kmcpv1alpha1.TransportType("http")
		spec.HTTPTransport = &kmcpv1alpha1.HTTPTransport{
			TargetPort: server.Local.HTTP.Port,
			TargetPath: server.Local.HTTP.Path,
		}
		if server.Local.HTTP.Port > 0 {
			spec.Deployment.Port = uint16(server.Local.HTTP.Port)
		}
	case api.TransportTypeStdio:
		spec.TransportType = kmcpv1alpha1.TransportType("stdio")
		spec.StdioTransport = &kmcpv1alpha1.StdioTransport{}
	default:
		return nil, fmt.Errorf("unsupported MCP transport type %q for %s", server.Local.TransportType, server.Name)
	}

	return &kmcpv1alpha1.MCPServer{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "kagent.dev/v1alpha1",
			Kind:       "MCPServer",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      MCPServerResourceName(server.Name),
			Namespace: namespace,
			Labels: map[string]string{
				"aregistry.ai/managed": "true",
			},
		},
		Spec: spec,
	}, nil
}

// translateAgentConfigMap creates a ConfigMap containing the mcp-servers.json for an agent
// This file is mounted into the agent's pod at /config/mcp-servers.json
// The BYO agent then reads this file and connects to the MCP servers
func (t *translator) translateAgentConfigMap(agent *api.Agent) (*corev1.ConfigMap, error) {
	// Use namespace from KAGENT_NAMESPACE env if set; otherwise leave empty
	// and let the runtime layer resolve from kubeconfig context.
	namespace := agent.Deployment.Env["KAGENT_NAMESPACE"]

	// Convert ResolvedMCPServers to JSON format expected by the Python agent
	serversJSON, err := json.MarshalIndent(agent.ResolvedMCPServers, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal MCP servers config: %w", err)
	}

	configMapName := AgentConfigMapName(agent.Name, agent.Version)

	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "agentregistry",
				"app.kubernetes.io/component":  "agent-config",
				"agentregistry.dev/agent":      sanitizeK8sName(agent.Name),
			},
		},
		Data: map[string]string{
			"mcp-servers.json": string(serversJSON),
		},
	}, nil
}

// AgentConfigMapName returns the ConfigMap name for an agent
func AgentConfigMapName(name, version string) string {
	base := fmt.Sprintf("%s-mcp-config", name)
	if version != "" {
		base = fmt.Sprintf("%s-%s-mcp-config", name, version)
	}
	return sanitizeK8sName(base)
}

func buildRemoteMCPURL(host string, port uint32, path string) string {
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

func AgentResourceName(name, version string) string {
	base := name
	if version != "" {
		base = fmt.Sprintf("%s-%s", name, version)
	}
	return sanitizeK8sName(base)
}

func RemoteMCPResourceName(name string) string {
	return sanitizeK8sName(name)
}

func MCPServerResourceName(name string) string {
	return sanitizeK8sName(name)
}

// sanitizeK8sName sanitizes a string to a valid Kubernetes name
func sanitizeK8sName(value string) string {
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
	return result
}
