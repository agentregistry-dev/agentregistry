package kubernetes

// kubernetesDeploymentAdapter serves Deployments onto a kagent-equipped
// Kubernetes cluster. The v1alpha1 surface (Apply / Remove / Logs /
// Discover / SupportedTargetKinds / Platform) lives in
// v1alpha1_adapter.go; this file holds only the struct + constructor
// shared across that surface and the supporting platform helpers in
// deployment_adapter_kubernetes_platform.go.
type kubernetesDeploymentAdapter struct{}

// NewKubernetesDeploymentAdapter constructs an adapter that resolves
// every per-call target cluster from the supplied v1alpha1.Provider's
// Spec.Config map. No up-front state — each Apply/Remove/Discover
// builds a fresh controller-runtime client from the provider config.
func NewKubernetesDeploymentAdapter() *kubernetesDeploymentAdapter {
	return &kubernetesDeploymentAdapter{}
}

func (a *kubernetesDeploymentAdapter) Platform() string { return "kubernetes" }
