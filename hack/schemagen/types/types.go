// Package types contains generation-only CRD roots.
//
// +groupName=agentregistry.solo.io
package types

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// +kubebuilder:object:root=true
type Agent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              v1alpha1.AgentSpec `json:"spec"`
}

// +kubebuilder:object:root=true
type Deployment struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              v1alpha1.DeploymentSpec `json:"spec"`
}

// +kubebuilder:object:root=true
type MCPServer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              v1alpha1.MCPServerSpec `json:"spec"`
}

// +kubebuilder:object:root=true
type Model struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              v1alpha1.ModelSpec `json:"spec"`
}

// +kubebuilder:object:root=true
type Plugin struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              v1alpha1.PluginSpec `json:"spec"`
}

// +kubebuilder:object:root=true
type Prompt struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              v1alpha1.PromptSpec `json:"spec"`
}

// +kubebuilder:object:root=true
type Runtime struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              v1alpha1.RuntimeSpec `json:"spec"`
}

// +kubebuilder:object:root=true
type Secret struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              v1alpha1.SecretSpec `json:"spec"`
}

// +kubebuilder:object:root=true
type Skill struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              v1alpha1.SkillSpec `json:"spec"`
}
