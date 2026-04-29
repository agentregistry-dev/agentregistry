package v1alpha1

// Skill is the typed envelope for kind=Skill resources.
type Skill struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata ObjectMeta `json:"metadata" yaml:"metadata"`
	Spec     SkillSpec  `json:"spec" yaml:"spec"`
	Status   Status     `json:"status,omitzero" yaml:"status,omitempty"`
}

// SkillSpec is the skill resource's declarative body.
type SkillSpec struct {
	Title       string      `json:"title,omitempty" yaml:"title,omitempty"`
	Description string      `json:"description,omitempty" yaml:"description,omitempty"`
	Repository  *Repository `json:"repository,omitempty" yaml:"repository,omitempty"`
}
