package v1alpha1

// RemoteMCPServer is the typed envelope for kind=RemoteMCPServer resources.
//
// A RemoteMCPServer points at an already-running MCP server endpoint. It
// is the peer of a Deployment in the lifecycle picture: Deployment runs a
// bundled server from an MCPServer template, while RemoteMCPServer just
// registers an endpoint the registry can call.
type RemoteMCPServer struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata ObjectMeta          `json:"metadata" yaml:"metadata"`
	Spec     RemoteMCPServerSpec `json:"spec" yaml:"spec"`
	Status   Status              `json:"status,omitzero" yaml:"status,omitempty"`
}

// RemoteMCPServerSpec is the body of a RemoteMCPServer. The validator
// rejects empty Remote — Type and URL are both required.
type RemoteMCPServerSpec struct {
	Title       string `json:"title,omitempty" yaml:"title,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// Remote is the connection endpoint of the running server.
	Remote MCPTransport `json:"remote" yaml:"remote"`
}
