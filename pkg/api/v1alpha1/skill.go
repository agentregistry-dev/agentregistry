package v1alpha1

// Skill is the typed envelope for kind=Skill resources.
type Skill struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata ObjectMeta  `json:"metadata" yaml:"metadata"`
	Spec     SkillSpec   `json:"spec" yaml:"spec"`
	Status   SkillStatus `json:"status,omitzero" yaml:"status,omitempty"`
}

func init() {
	MustRegisterKind[*Skill, SkillSpec](KindSkill)
}

// SkillSpec is the skill resource's declarative body.
type SkillSpec struct {
	Title       string       `json:"title,omitempty" yaml:"title,omitempty"`
	Description string       `json:"description,omitempty" yaml:"description,omitempty"`
	Source      *SkillSource `json:"source,omitempty" yaml:"source,omitempty"`
}

// SkillSource is the distribution origin of a skill. Currently just a
// git repository where the skill content lives. Future distribution
// channels (e.g. published artifact) would land here.
type SkillSource struct {
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

	// ResolvedSource is the controller's immutable pin of the skill's git
	// source (the concrete commit the source ref resolved to).
	ResolvedSource *SkillResolvedSource `json:"resolvedSource,omitempty" yaml:"resolvedSource,omitempty"`
}

// SkillResolvedSource records the concrete commit the Skill controller pinned
// the skill's git source to. It is the reproducibility anchor: deploys
// materialize from this pin, not from the (possibly moving) ref the user gave.
type SkillResolvedSource struct {
	// Commit is the resolved full git commit SHA.
	Commit string `json:"commit,omitempty" yaml:"commit,omitempty"`
}
