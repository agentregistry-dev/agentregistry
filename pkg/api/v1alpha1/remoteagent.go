package v1alpha1

// RemoteAgent is the typed envelope for kind=RemoteAgent resources.
//
// A RemoteAgent points at an already-running agent endpoint. Symmetric to
// RemoteMCPServer: registry registers the endpoint without managing its
// lifecycle.
type RemoteAgent struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata ObjectMeta      `json:"metadata" yaml:"metadata"`
	Spec     RemoteAgentSpec `json:"spec" yaml:"spec"`
	Status   Status          `json:"status,omitzero" yaml:"status,omitempty"`
}

// RemoteAgentSpec is the body of a RemoteAgent. The validator rejects
// empty Remote — Type and URL are both required.
type RemoteAgentSpec struct {
	Title       string `json:"title,omitempty" yaml:"title,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// Remote is the connection endpoint of the running agent.
	Remote AgentRemote `json:"remote,omitempty" yaml:"remote,omitempty"`
}
