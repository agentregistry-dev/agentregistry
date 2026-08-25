package v1alpha1

const (
	// TypeMicrosoftFoundry discovers agents from a Foundry project.
	TypeMicrosoftFoundry = "MicrosoftFoundry"
	// TypeMicrosoftCopilotStudio discovers agents from a Power Platform environment.
	TypeMicrosoftCopilotStudio = "MicrosoftCopilotStudio"
)

func init() {
	KnownRuntimeTypes[TypeMicrosoftFoundry] = struct{}{}
	KnownRuntimeTypes[TypeMicrosoftCopilotStudio] = struct{}{}
}

// MicrosoftRuntimeAuth configures authentication for Microsoft runtimes.
type MicrosoftRuntimeAuth struct {
	OIDC *RuntimeOIDCAuth `json:"oidc,omitempty" yaml:"oidc,omitempty"`
}

// RuntimeOIDCAuth configures client-credentials authentication for a runtime.
type RuntimeOIDCAuth struct {
	Issuer             string        `json:"issuer,omitempty" yaml:"issuer,omitempty"`
	ClientID           string        `json:"clientId,omitempty" yaml:"clientId,omitempty"`
	Scope              string        `json:"scope,omitempty" yaml:"scope,omitempty"`
	ClientSecretRef    *SecretKeyRef `json:"clientSecretRef,omitempty" yaml:"clientSecretRef,omitempty"`
	InsecureSkipVerify *bool         `json:"insecureSkipVerify,omitempty" yaml:"insecureSkipVerify,omitempty"`
}

// MicrosoftFoundryRuntimeConfig identifies a Foundry project for discovery.
type MicrosoftFoundryRuntimeConfig struct {
	ProjectEndpoint string               `json:"projectEndpoint,omitempty" yaml:"projectEndpoint,omitempty"`
	SubscriptionID  string               `json:"subscriptionId,omitempty" yaml:"subscriptionId,omitempty"`
	ResourceGroup   string               `json:"resourceGroup,omitempty" yaml:"resourceGroup,omitempty"`
	Auth            MicrosoftRuntimeAuth `json:"auth" yaml:"auth"`
}

// MicrosoftCopilotStudioRuntimeConfig identifies a Copilot Studio environment for discovery.
type MicrosoftCopilotStudioRuntimeConfig struct {
	EnvironmentID string               `json:"environmentId,omitempty" yaml:"environmentId,omitempty"`
	DataEndpoint  string               `json:"dataEndpoint,omitempty" yaml:"dataEndpoint,omitempty"`
	Auth          MicrosoftRuntimeAuth `json:"auth" yaml:"auth"`
}
