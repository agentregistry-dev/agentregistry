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
	Title       string         `json:"title,omitempty" yaml:"title,omitempty"`
	Category    string         `json:"category,omitempty" yaml:"category,omitempty"`
	Description string         `json:"description,omitempty" yaml:"description,omitempty"`
	WebsiteURL  string         `json:"websiteUrl,omitempty" yaml:"websiteUrl,omitempty"`
	Readme      *Readme        `json:"readme,omitempty" yaml:"readme,omitempty"`
	Repository  *Repository    `json:"repository,omitempty" yaml:"repository,omitempty"`
	Packages    []SkillPackage `json:"packages,omitempty" yaml:"packages,omitempty"`
	Remotes     []SkillRemote  `json:"remotes,omitempty" yaml:"remotes,omitempty"`
}

// SkillPackage describes a distributable package of the skill.
type SkillPackage struct {
	RegistryType string         `json:"registryType" yaml:"registryType"`
	Identifier   string         `json:"identifier" yaml:"identifier"`
	Version      string         `json:"version" yaml:"version"`
	Transport    TransportProto `json:"transport" yaml:"transport"`
}

// SkillRemote describes a remote endpoint at which the skill content is reachable.
type SkillRemote struct {
	URL string `json:"url,omitempty" yaml:"url,omitempty"`
}
