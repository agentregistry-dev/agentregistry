package v1alpha1

// Repository is a source-code location shared by several resource kinds.
type Repository struct {
	URL       string `json:"url,omitempty" yaml:"url,omitempty"`
	Subfolder string `json:"subfolder,omitempty" yaml:"subfolder,omitempty"`
}
