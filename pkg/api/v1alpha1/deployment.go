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

func init() {
	MustRegisterKind[*Deployment, DeploymentSpec](
		KindDeployment,
		WithMutableObjectStorage(),
	)
}

// Deployment origin annotations distinguish registry-managed Deployment rows
// from provider-discovered rows materialized into the same table.
const (
	DeploymentOriginAnnotation                = "agentregistry.solo.io/origin"
	DeploymentDiscoveredRuntimeAnnotation     = "agentregistry.solo.io/discovered-runtime"
	DeploymentDiscoveredRuntimeTypeAnnotation = "agentregistry.solo.io/discovered-runtime-type"
	DeploymentOriginManaged                   = "managed"
	DeploymentOriginDiscovered                = "discovered"
)

// IsDiscoveredDeployment reports whether a Deployment row was materialized from
// provider discovery rather than authored as registry-managed desired state.
func IsDiscoveredDeployment(deployment *Deployment) bool {
	if deployment == nil || deployment.Metadata.Annotations == nil {
		return false
	}
	return deployment.Metadata.Annotations[DeploymentOriginAnnotation] == DeploymentOriginDiscovered
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
	// Harness selects a compatible harness for Agent deployments and configures
	// rollout-specific harness policy. Omitted for BYO image/source Agent
	// deployments and MCPServer deployments.
	Harness *DeploymentHarness `json:"harness,omitempty" yaml:"harness,omitempty"`
}

// DeploymentHarness selects the concrete harness to run for one Deployment.
// The target Agent declares compatibility; the Runtime supplies concrete
// runner support such as container images.
type DeploymentHarness struct {
	// Type is the selected harness family, e.g. "claude-code", "codex".
	Type string `json:"type" yaml:"type"`

	// Version pins the selected harness version for this rollout. Empty asks the
	// target Runtime to use its default for Type.
	Version string `json:"version,omitempty" yaml:"version,omitempty"`

	// PermissionMode controls the harness tool-permission posture, e.g.
	// "default", "acceptEdits", "bypassPermissions". Empty defaults to
	// "bypassPermissions" for headless harness runtimes (no interactive
	// approval is possible); subject to security review.
	PermissionMode string `json:"permissionMode,omitempty" yaml:"permissionMode,omitempty"`
}
