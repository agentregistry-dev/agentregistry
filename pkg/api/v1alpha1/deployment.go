package v1alpha1

// Deployment is the typed envelope for kind=Deployment resources.
//
// Deployment's metadata.name is independent from the thing it deploys
// (Spec.TemplateRef), so multiple Deployments can target the same Agent or
// MCPServer with different user-chosen names, runtimes, and configs. Identity
// is namespace/name; the deployed content is pinned separately through
// spec.targetRef.tag.
type Deployment struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata ObjectMeta     `json:"metadata" yaml:"metadata"`
	Spec     DeploymentSpec `json:"spec" yaml:"spec"`
	Status   Status         `json:"status,omitzero" yaml:"status,omitempty"`
}

// DeploymentDesiredState lifecycle intents. Empty is equivalent to
// DesiredStateDeployed.
const (
	DesiredStateDeployed   = "deployed"
	DesiredStateUndeployed = "undeployed"
)

// DeploymentSpec is the deployment resource's declarative body.
//
// TargetRef is required and must name a top-level Agent or MCPServer. The
// referenced resource's spec is the source of truth for what to run; this
// Deployment contributes only runtime overrides (env, runtimeConfig) and
// lifecycle intent (desiredState).
//
// RuntimeRef is required and must name a top-level Runtime. The Runtime
// resolves how/where the target is executed (local daemon, kubernetes, etc.).
type DeploymentSpec struct {
	TargetRef    ResourceRef `json:"targetRef" yaml:"targetRef"`
	RuntimeRef   ResourceRef `json:"runtimeRef" yaml:"runtimeRef"`
	DesiredState string      `json:"desiredState,omitempty" yaml:"desiredState,omitempty"`
	// DeploymentRefs declaratively binds this Deployment to other
	// Deployments — e.g. an Agent Deployment binding to the MCPServer
	// Deployments whose status should feed its runtime config. Stored
	// and structurally validated; binding semantics are owned by the
	// kind's reconciler.
	DeploymentRefs []DeploymentRef   `json:"deploymentRefs,omitempty" yaml:"deploymentRefs,omitempty"`
	Env            map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
	RuntimeConfig  map[string]any    `json:"runtimeConfig,omitempty" yaml:"runtimeConfig,omitempty"`
}
