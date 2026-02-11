package api

import (
	v1alpha2 "github.com/kagent-dev/kagent/go/api/v1alpha2"
	kmcpv1alpha1 "github.com/kagent-dev/kmcp/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

// DesiredState represents the desired set of MCPServevrs the user wishes to run locally
type DesiredState struct {
	MCPServers []*MCPServer `json:"mcpServers"`
	Agents     []*Agent     `json:"agents"`
}

// Agent represents a single Agent configuration
type Agent struct {
	Name       string          `json:"name"`
	Version    string          `json:"version"`
	Deployment AgentDeployment `json:"deployment"`
	// ResolvedMCPServers contains the MCP server connection info for this agent
	// Used to generate ConfigMap for Kubernetes deployments
	ResolvedMCPServers []ResolvedMCPServerConfig `json:"resolvedMCPServers,omitempty"`
}

// This gets saved into the mcp-servers.json file for each agent
type ResolvedMCPServerConfig struct {
	Name    string            `json:"name"`
	Type    string            `json:"type"` // "command" or "remote"
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// MCPServer represents a single MCPServer configuration
type MCPServer struct {
	// Name is the unique name of the MCPServer
	Name string `json:"name"`
	// MCPServerType represents whether the MCP server is remote or local
	MCPServerType MCPServerType `json:"mcpServerType"`
	// Remote defines how to route to a remote MCP server
	Remote *RemoteMCPServer `json:"remote,omitempty"`
	// Local defines how to deploy the MCP server locally
	Local *LocalMCPServer `json:"local,omitempty"`
	// Namespace is the target namespace for Kubernetes deployments (optional, defaults to "default")
	Namespace string `json:"namespace,omitempty"`
}

type MCPServerType string

const (
	// MCPServerTypeRemote indicates that the MCP server is hosted remotely
	MCPServerTypeRemote MCPServerType = "remote"

	// MCPServerTypeLocal indicates that the MCP server is hosted locally
	MCPServerTypeLocal MCPServerType = "local"
)

// RemoteMCPServer represents the configuration for connecting to a remotely hosted MCPServer
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

// LocalMCPServer represents the configuration for running an MCPServer locally
type LocalMCPServer struct {
	// Deployment defines how to deploy the MCP server
	Deployment MCPServerDeployment `json:"deployment"`
	// TransportType defines the type of mcp server being run
	TransportType TransportType `json:"transportType"`
	// HTTP defines the configuration for an HTTP transport.(only for TransportTypeHTTP)
	HTTP *HTTPTransport `json:"http,omitempty"`
}

// HTTPTransport defines the configuration for an HTTP transport
type HTTPTransport struct {
	Port uint32 `json:"port"`
	Path string `json:"path,omitempty"`
}

// MCPServerTransportType defines the type of transport for the MCP server.
type TransportType string

const (
	// TransportTypeStdio indicates that the MCP server uses standard input/output for communication.
	TransportTypeStdio TransportType = "stdio"

	// TransportTypeHTTP indicates that the MCP server uses Streamable HTTP for communication.
	TransportTypeHTTP TransportType = "http"
)

// MCPServerDeployment
type MCPServerDeployment struct {
	// Image defines the container image to to deploy the MCP server.
	Image string `json:"image,omitempty"`

	// Cmd defines the command to run in the container to start the mcp server.
	Cmd string `json:"cmd,omitempty"`

	// Args defines the arguments to pass to the command.
	Args []string `json:"args,omitempty"`

	// Env defines the environment variables to set in the container.
	Env map[string]string `json:"env,omitempty"`
}

type AgentDeployment struct {
	Image string            `json:"image,omitempty"`
	Env   map[string]string `json:"env,omitempty"`
	Port  uint16            `json:"port,omitempty"`
}

type AIRuntimeConfig struct {
	Local      *LocalRuntimeConfig
	Kubernetes *KubernetesRuntimeConfig

	Type RuntimeConfigType
}

type RuntimeConfigType string

const (
	RuntimeConfigTypeLocal      RuntimeConfigType = "local"
	RuntimeConfigTypeKubernetes RuntimeConfigType = "kubernetes"
)

type KubernetesRuntimeConfig struct {
	Agents           []*v1alpha2.Agent           `json:"agents"`
	RemoteMCPServers []*v1alpha2.RemoteMCPServer `json:"remoteMCPServers"`
	MCPServers       []*kmcpv1alpha1.MCPServer   `json:"mcpServers"`
	ConfigMaps       []*corev1.ConfigMap         `json:"configMaps,omitempty"`
}
