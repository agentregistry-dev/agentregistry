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
	// metadata is part of Deployment.
	// +required
	Metadata ObjectMeta `json:"metadata" yaml:"metadata"`
	// spec is part of Deployment.
	// +required
	Spec DeploymentSpec `json:"spec" yaml:"spec"`
	// status is part of Deployment.
	// +optional
	Status Status `json:"status,omitzero" yaml:"status,omitempty"`
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
// RuntimeRef is required and must name a top-level Runtime. The embedding
// application registers the adapters that resolve how and where the target is
// executed.
type DeploymentSpec struct {
	// targetRef is part of DeploymentSpec.
	// +required
	// +kubebuilder:validation:MinProperties=1
	TargetRef ResourceRef `json:"targetRef" yaml:"targetRef"`
	// runtimeRef is part of DeploymentSpec.
	// +required
	// +kubebuilder:validation:MinProperties=1
	RuntimeRef ResourceRef `json:"runtimeRef" yaml:"runtimeRef"`
	// modelRef selects the tagged Model for this Deployment. When omitted from
	// a harness Agent Deployment, it defaults to Model/default@latest in the
	// Deployment namespace. It remains optional with no implicit selection for
	// non-harness Agent and MCPServer Deployments. Provider, endpoint, and auth
	// configuration remain on the referenced Model.
	// +optional
	ModelRef *ModelRef `json:"modelRef,omitempty" yaml:"modelRef,omitempty"`
	// desiredState is part of DeploymentSpec.
	// +optional
	DesiredState string `json:"desiredState,omitempty" yaml:"desiredState,omitempty"`
	// deploymentRefs declaratively binds this Deployment to other
	// Deployments — e.g. an Agent Deployment binding to the MCPServer
	// Deployments whose status should feed its runtime config. Stored
	// and structurally validated; binding semantics are owned by the
	// kind's reconciler.
	// +optional
	DeploymentRefs []DeploymentRef `json:"deploymentRefs,omitempty" yaml:"deploymentRefs,omitempty"`
	// env is part of DeploymentSpec.
	// +optional
	Env map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
	// runtimeConfig carries adapter-specific per-Deployment configuration.
	// Its schema is selected by the resolved Runtime type and validated by that
	// runtime's DeploymentAdapter.
	// +optional
	RuntimeConfig map[string]any `json:"runtimeConfig,omitempty" yaml:"runtimeConfig,omitempty"`
	// harness selects a compatible harness for Agent deployments and configures
	// rollout-specific harness policy. Omitted for BYO image/source Agent
	// deployments and MCPServer deployments.
	// +optional
	Harness *DeploymentHarness `json:"harness,omitempty" yaml:"harness,omitempty"`
}

// EffectiveModelRef returns the explicit ModelRef or the conventional
// namespace-scoped default for a harness Agent Deployment. It returns nil for
// non-harness Agent and MCPServer Deployments that omit ModelRef.
func (s *DeploymentSpec) EffectiveModelRef() *ModelRef {
	if s == nil {
		return nil
	}
	if s.ModelRef != nil {
		return s.ModelRef
	}
	if s.TargetRef.Kind == KindAgent && s.Harness != nil {
		return &ModelRef{Name: DefaultModelName}
	}
	return nil
}

// DeploymentHarness selects the concrete harness to run for one Deployment.
// The target Agent declares compatibility; the Runtime supplies concrete
// runner support such as container images.
type DeploymentHarness struct {
	// type is the selected harness family, e.g. "claude-code", "codex".
	// +required
	// +kubebuilder:validation:MinLength=1
	Type string `json:"type" yaml:"type"`

	// permissionMode controls the harness tool-permission posture, e.g.
	// "default", "acceptEdits", "bypassPermissions". Empty defaults to
	// "bypassPermissions" for headless harness runtimes (no interactive
	// approval is possible); subject to security review.
	// +optional
	PermissionMode string `json:"permissionMode,omitempty" yaml:"permissionMode,omitempty"`
}
