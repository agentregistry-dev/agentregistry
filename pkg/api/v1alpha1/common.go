package v1alpha1

// Repository is a source-code location shared by several resource kinds.
type Repository struct {
	URL       string `json:"url,omitempty" yaml:"url,omitempty"`
	Source    string `json:"source,omitempty" yaml:"source,omitempty"`
	ID        string `json:"id,omitempty" yaml:"id,omitempty"`
	Subfolder string `json:"subfolder,omitempty" yaml:"subfolder,omitempty"`
}

// TransportProto is the minimal transport discriminator used inside package
// references (Agent and Skill). MCPServer uses its own richer Transport type
// that carries URL and headers for remote transports.
type TransportProto struct {
	Type string `json:"type" yaml:"type"`
}
