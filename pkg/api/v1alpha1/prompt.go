package v1alpha1

// Prompt is the typed envelope for kind=Prompt resources.
type Prompt struct {
	TypeMeta `json:",inline" yaml:",inline"`
	// metadata is part of Prompt.
	// +required
	Metadata ObjectMeta `json:"metadata" yaml:"metadata"`
	// spec is part of Prompt.
	// +required
	Spec PromptSpec `json:"spec" yaml:"spec"`
	// status is part of Prompt.
	// +optional
	Status Status `json:"status,omitzero" yaml:"status,omitempty"`
}

func init() {
	MustRegisterKind[*Prompt, PromptSpec](KindPrompt)
}

// PromptSpec is the prompt resource's declarative body. Content holds the
// prompt text inline; for large bodies or binary assets, use references via
// a Skill resource instead.
type PromptSpec struct {
	// description is part of PromptSpec.
	// +optional
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	// content is part of PromptSpec.
	// +optional
	Content string `json:"content,omitempty" yaml:"content,omitempty"`

	// iconUrl is the image a catalog UI shows for this prompt. Either an
	// absolute https:// URL or a root-relative path served by the UI.
	// +optional
	IconURL string `json:"iconUrl,omitempty" yaml:"iconUrl,omitempty"`
}
