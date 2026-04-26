package v1alpha1

// Prompt is the typed envelope for kind=Prompt resources.
type Prompt struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata ObjectMeta `json:"metadata" yaml:"metadata"`
	Spec     PromptSpec `json:"spec" yaml:"spec"`
	Status   Status     `json:"status,omitzero" yaml:"status,omitempty"`
}

// PromptSpec is the prompt resource's declarative body. Content holds the
// prompt text inline; for large bodies or binary assets, use references via
// a Skill resource instead.
type PromptSpec struct {
	Description string  `json:"description,omitempty" yaml:"description,omitempty"`
	Content     string  `json:"content,omitempty" yaml:"content,omitempty"`
	Readme      *Readme `json:"readme,omitempty" yaml:"readme,omitempty"`
}
