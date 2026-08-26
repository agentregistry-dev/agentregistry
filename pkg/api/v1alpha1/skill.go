package v1alpha1

// Skill is the typed envelope for kind=Skill resources.
type Skill struct {
	TypeMeta `json:",inline" yaml:",inline"`
	// metadata is part of Skill.
	// +required
	Metadata ObjectMeta `json:"metadata" yaml:"metadata"`
	// spec is part of Skill.
	// +required
	Spec SkillSpec `json:"spec" yaml:"spec"`
	// status is part of Skill.
	// +optional
	Status SkillStatus `json:"status,omitzero" yaml:"status,omitempty"`
}

func init() {
	MustRegisterKind[*Skill, SkillSpec](KindSkill)
}

// SkillSpec is the skill resource's declarative body.
type SkillSpec struct {
	// title is part of SkillSpec.
	// +optional
	Title string `json:"title,omitempty" yaml:"title,omitempty"`
	// description is part of SkillSpec.
	// +optional
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// iconUrl is the image a catalog UI shows for this skill. Either an
	// absolute https:// URL or a root-relative path served by the UI.
	// +optional
	IconURL string `json:"iconUrl,omitempty" yaml:"iconUrl,omitempty"`

	// source is part of SkillSpec.
	// +optional
	Source *SkillSource `json:"source,omitempty" yaml:"source,omitempty"`
}

// SkillSource is the distribution origin of a skill. Currently just a
// git repository where the skill content lives. Future distribution
// channels (e.g. published artifact) would land here.
type SkillSource struct {
	// repository is part of SkillSource.
	// +optional
	Repository *Repository `json:"repository,omitempty" yaml:"repository,omitempty"`
}

// SkillStatus is the Skill observed-state subresource, written by the Skill
// controller out of band of the API write. It embeds the shared Status
// (conditions + observedGeneration) and records the controller's immutable pin
// of the skill's git source — mirroring the Plugin resolve-and-pin model so a
// harness deploy can materialize the skill from a fixed commit.
//
// Readiness: absence of Ready=True (or ResolvedSource==nil) means "not yet
// resolved". The controller sets Ready=False/Progressing on first observe,
// Ready=True/Resolved once the source is pinned, and Ready=False with a
// specific reason (SourceUnresolvable, SourceInvalid) on failure.
type SkillStatus struct {
	Status `json:",inline" yaml:",inline"`

	// resolvedSource is the controller's immutable pin of the skill's git
	// source (the concrete commit the source ref resolved to).
	// +optional
	ResolvedSource *SkillResolvedSource `json:"resolvedSource,omitempty" yaml:"resolvedSource,omitempty"`
}

// SkillResolvedSource records the concrete commit the Skill controller pinned
// the skill's git source to. It is the reproducibility anchor: deploys
// materialize from this pin, not from the (possibly moving) ref the user gave.
type SkillResolvedSource struct {
	// commit is the resolved full git commit SHA.
	// +optional
	Commit string `json:"commit,omitempty" yaml:"commit,omitempty"`
}
