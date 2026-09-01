package kagent

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	agentTypeBYO                    = "BYO"
	remoteMCPProtocolStreamableHTTP = "STREAMABLE_HTTP"
	transportTypeStdio              = "stdio"
	transportTypeHTTP               = "http"
)

// These private payload types preserve the Kagent v0.10.0-rc3 REST wire contract
// without importing Kagent controller modules.
type agentPayload struct {
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              agentPayloadSpec `json:"spec,omitempty"`
	Status            struct{}         `json:"status,omitempty"`
}

type agentPayloadSpec struct {
	Type        string               `json:"type,omitempty"`
	BYO         *byoAgentPayloadSpec `json:"byo,omitempty"`
	Description string               `json:"description,omitempty"`
}

type byoAgentPayloadSpec struct {
	Deployment *byoDeploymentPayload `json:"deployment,omitempty"`
}

type byoDeploymentPayload struct {
	Image            string                        `json:"image,omitempty"`
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
	Labels           map[string]string             `json:"labels,omitempty"`
	Env              []corev1.EnvVar               `json:"env,omitempty"`
	Tolerations      []corev1.Toleration           `json:"tolerations,omitempty"`
	Affinity         *corev1.Affinity              `json:"affinity,omitempty"`
	NodeSelector     map[string]string             `json:"nodeSelector,omitempty"`
}

type remoteMCPServerPayload struct {
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              remoteMCPServerPayloadSpec `json:"spec,omitempty"`
	Status            struct{}                   `json:"status,omitempty"`
}

type remoteMCPServerPayloadSpec struct {
	Description string `json:"description"`
	Protocol    string `json:"protocol,omitempty"`
	URL         string `json:"url"`
}

type mcpServerPayload struct {
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              mcpServerPayloadSpec `json:"spec,omitempty"`
	Status            struct{}             `json:"status,omitempty"`
}

type mcpServerPayloadSpec struct {
	Deployment     mcpServerDeploymentPayload `json:"deployment"`
	TransportType  string                     `json:"transportType,omitempty"`
	StdioTransport *stdioTransportPayload     `json:"stdioTransport,omitempty"`
	HTTPTransport  *httpTransportPayload      `json:"httpTransport,omitempty"`
}

type stdioTransportPayload struct{}

type httpTransportPayload struct {
	TargetPort uint32 `json:"targetPort,omitempty"`
	TargetPath string `json:"path,omitempty"`
}

type mcpServerDeploymentPayload struct {
	Image            string                        `json:"image,omitempty"`
	Port             uint16                        `json:"port,omitempty"`
	Cmd              string                        `json:"cmd,omitempty"`
	Args             []string                      `json:"args,omitempty"`
	Env              map[string]string             `json:"env,omitempty"`
	SecretRefs       []corev1.LocalObjectReference `json:"secretRefs,omitempty"`
	Labels           map[string]string             `json:"labels,omitempty"`
	Tolerations      []corev1.Toleration           `json:"tolerations,omitempty"`
	Affinity         *corev1.Affinity              `json:"affinity,omitempty"`
	NodeSelector     map[string]string             `json:"nodeSelector,omitempty"`
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
}
