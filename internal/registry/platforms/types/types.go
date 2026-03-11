package types

import (
	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
	v1alpha2 "github.com/kagent-dev/kagent/go/api/v1alpha2"
	kmcpv1alpha1 "github.com/kagent-dev/kmcp/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"

	composetypes "github.com/compose-spec/compose-go/v2/types"
)

type DesiredState struct {
	MCPServers []*MCPServer `json:"mcpServers"`
	Agents     []*Agent     `json:"agents"`
}

type ResolvedAgentConfig struct {
	Agent                   *Agent
	ResolvedPlatformServers []*MCPServer
	ResolvedConfigServers   []ResolvedMCPServerConfig
	PythonConfigServers     []common.PythonMCPServer
}

type Agent struct {
	Name               string                    `json:"name"`
	Version            string                    `json:"version"`
	DeploymentID       string                    `json:"deploymentId,omitempty"`
	Deployment         AgentDeployment           `json:"deployment"`
	ResolvedMCPServers []ResolvedMCPServerConfig `json:"resolvedMCPServers,omitempty"`
	Skills             []AgentSkillRef           `json:"skills,omitempty"`
}

type AgentSkillRef struct {
	Name    string `json:"name,omitempty"`
	Image   string `json:"image,omitempty"`
	RepoURL string `json:"repoURL,omitempty"`
	Ref     string `json:"ref,omitempty"`
	Path    string `json:"path,omitempty"`
}

type ResolvedMCPServerConfig struct {
	Name    string            `json:"name"`
	Type    string            `json:"type"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

type MCPServer struct {
	Name          string           `json:"name"`
	DeploymentID  string           `json:"deploymentId,omitempty"`
	MCPServerType MCPServerType    `json:"mcpServerType"`
	Remote        *RemoteMCPServer `json:"remote,omitempty"`
	Local         *LocalMCPServer  `json:"local,omitempty"`
	Namespace     string           `json:"namespace,omitempty"`
}

type MCPServerType string

const (
	MCPServerTypeRemote MCPServerType = "remote"
	MCPServerTypeLocal  MCPServerType = "local"
)

type RemoteMCPServer struct {
	Host    string
	Port    uint32
	Path    string
	Headers []HeaderValue
}

type HeaderValue struct {
	Name  string
	Value string
}

type LocalMCPServer struct {
	Deployment    MCPServerDeployment `json:"deployment"`
	TransportType TransportType       `json:"transportType"`
	HTTP          *HTTPTransport      `json:"http,omitempty"`
}

type HTTPTransport struct {
	Port uint32 `json:"port"`
	Path string `json:"path,omitempty"`
}

type TransportType string

const (
	TransportTypeStdio TransportType = "stdio"
	TransportTypeHTTP  TransportType = "http"
)

type MCPServerDeployment struct {
	Image string            `json:"image,omitempty"`
	Cmd   string            `json:"cmd,omitempty"`
	Args  []string          `json:"args,omitempty"`
	Env   map[string]string `json:"env,omitempty"`
}

type AgentDeployment struct {
	Image string            `json:"image,omitempty"`
	Env   map[string]string `json:"env,omitempty"`
	Port  uint16            `json:"port,omitempty"`
}

type KubernetesPlatformConfig struct {
	Agents           []*v1alpha2.Agent           `json:"agents"`
	RemoteMCPServers []*v1alpha2.RemoteMCPServer `json:"remoteMCPServers"`
	MCPServers       []*kmcpv1alpha1.MCPServer   `json:"mcpServers"`
	ConfigMaps       []*corev1.ConfigMap         `json:"configMaps,omitempty"`
}

type DockerComposeConfig = composetypes.Project

type LocalPlatformConfig struct {
	DockerCompose *DockerComposeConfig
	AgentGateway  *AgentGatewayConfig
}
