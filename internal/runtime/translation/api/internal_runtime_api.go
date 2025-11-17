package api

// DestiredState represents the desired set of MCPServers and Agents the user wishes to run locally
type DesiredState struct {
	MCPServers []*MCPServer `json:"mcpServers"`
	Agents     []*Agent     `json:"agents"`
}

// MCPServer represents a single MCPServer configuration
type MCPServer struct {
	// Name is the unique name of the MCPServer
	Name string `json:"name"`
	// ResourceType represents whether the MCP server is remote or local
	ResourceType ResourceType `json:"resourceType"`
	// Remote defines how to route to a remote MCP server
	Remote *RemoteMCPServer `json:"remote,omitempty"`
	// Local defines how to deploy the MCP server locally
	Local *LocalMCPServer `json:"local,omitempty"`
}

type ResourceType string

const (
	// ResourceTypeRemote indicates that the resource server is hosted remotely
	ResourceTypeRemote ResourceType = "remote"

	// ResourceTypeLocal indicates that the resource server is hosted locally
	ResourceTypeLocal ResourceType = "local"
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
	Deployment ContainerDeployment `json:"deployment"`
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

type Agent struct {
	// Name is the unique name of the Agent
	Name string `json:"name"`
	// ResourceType represents whether the agent is remote or local
	ResourceType ResourceType `json:"resourceType"`
	// Remote defines how to route to a remote MCP server
	Remote *RemoteAgent `json:"remote,omitempty"`
	// Local defines how to deploy the MCP server locally
	Local *LocalAgent `json:"local,omitempty"`
}

// RemoteAgent represents the configuration for connecting to a remotely hosted Agent
// Note: identical to RemoteMCPServer
type RemoteAgent RemoteMCPServer

// LocalAgent represents the configuration for running an Agent locally
type LocalAgent struct {
	// Deployment defines how to deploy the MCP server
	Deployment ContainerDeployment `json:"deployment"`
	// HTTP defines the configuration connecting to agents over an HTTP transport (Agents only support HTTP transport)
	HTTP *HTTPTransport `json:"http,omitempty"`
}

// ContainerDeployment
type ContainerDeployment struct {
	// Image defines the container image to to deploy the MCP server.
	Image string `json:"image,omitempty"`

	// Cmd defines the command to run in the container to start the mcp server.
	Cmd string `json:"cmd,omitempty"`

	// Args defines the arguments to pass to the command.
	Args []string `json:"args,omitempty"`

	// Env defines the environment variables to set in the container.
	Env map[string]string `json:"env,omitempty"`
}
