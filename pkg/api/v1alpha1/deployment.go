package v1alpha1

// Deployment is the typed envelope for kind=Deployment resources.
//
// Deployment's metadata.name is independent from the thing it deploys
// (Spec.TemplateRef), so multiple Deployments can target the same Agent or
// MCPServer with different user-chosen names, providers, and configs. Identity
// follows the same (name, version) composite-PK model as every other kind.
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
// Deployment contributes only runtime overrides (env, providerConfig) and
// lifecycle intent (desiredState).
//
// ProviderRef is required and must name a top-level Provider. The Provider
// resolves how/where the target is executed (local daemon, kubernetes, etc.).
type DeploymentSpec struct {
	TargetRef      ResourceRef       `json:"targetRef" yaml:"targetRef"`
	ProviderRef    ResourceRef       `json:"providerRef" yaml:"providerRef"`
	DesiredState   string            `json:"desiredState,omitempty" yaml:"desiredState,omitempty"`
	Env            map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
	ProviderConfig map[string]any    `json:"providerConfig,omitempty" yaml:"providerConfig,omitempty"`
}
