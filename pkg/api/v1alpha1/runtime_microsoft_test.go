package v1alpha1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMicrosoftRuntimeConfigDecode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		out  any
		want any
	}{
		{
			name: "foundry",
			raw:  `{"projectEndpoint":"https://foundry.example/projects/p1","subscriptionId":"sub-1","resourceGroup":"rg-1","auth":{"oidc":{"issuer":"https://login.microsoftonline.com/tenant/v2.0","clientId":"client-1","clientSecretRef":{"name":"credentials","key":"clientSecret"}}}}`,
			out:  &MicrosoftFoundryRuntimeConfig{},
			want: &MicrosoftFoundryRuntimeConfig{
				ProjectEndpoint: "https://foundry.example/projects/p1",
				SubscriptionID:  "sub-1",
				ResourceGroup:   "rg-1",
				Auth: MicrosoftRuntimeAuth{OIDC: &RuntimeOIDCAuth{
					Issuer:          "https://login.microsoftonline.com/tenant/v2.0",
					ClientID:        "client-1",
					ClientSecretRef: &SecretKeyRef{Name: "credentials", Key: "clientSecret"},
				}},
			},
		},
		{
			name: "copilot studio",
			raw:  `{"environmentId":"env-1","dataEndpoint":"https://org.crm.dynamics.com","auth":{"oidc":{"issuer":"https://login.microsoftonline.com/tenant/v2.0","clientId":"client-1","clientSecretRef":{"name":"credentials","key":"clientSecret"}}}}`,
			out:  &MicrosoftCopilotStudioRuntimeConfig{},
			want: &MicrosoftCopilotStudioRuntimeConfig{
				EnvironmentID: "env-1",
				DataEndpoint:  "https://org.crm.dynamics.com",
				Auth: MicrosoftRuntimeAuth{OIDC: &RuntimeOIDCAuth{
					Issuer:          "https://login.microsoftonline.com/tenant/v2.0",
					ClientID:        "client-1",
					ClientSecretRef: &SecretKeyRef{Name: "credentials", Key: "clientSecret"},
				}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.NoError(t, json.Unmarshal([]byte(tt.raw), tt.out))
			require.Equal(t, tt.want, tt.out)
		})
	}
}

func TestMicrosoftRuntimeValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		runtimeType string
		config      map[string]any
		wantErr     string
	}{
		{
			name:        "foundry",
			runtimeType: TypeMicrosoftFoundry,
			config: map[string]any{
				"projectEndpoint": "https://foundry.example/projects/p1",
				"auth":            validMicrosoftAuth(),
			},
		},
		{
			name:        "copilot studio",
			runtimeType: TypeMicrosoftCopilotStudio,
			config: map[string]any{
				"environmentId": "env-1", "dataEndpoint": "https://org.crm.dynamics.com",
				"auth": validMicrosoftAuth(),
			},
		},
		{
			name:        "rejects missing secret key",
			runtimeType: TypeMicrosoftFoundry,
			config: map[string]any{
				"projectEndpoint": "https://foundry.example/projects/p1",
				"auth": map[string]any{"oidc": map[string]any{
					"issuer": "https://login.microsoftonline.com/tenant/v2.0", "clientId": "client",
					"clientSecretRef": map[string]any{"name": "credentials"},
				}},
			},
			wantErr: "clientSecretRef.key",
		},
		{
			name:        "rejects issuer without tenant path",
			runtimeType: TypeMicrosoftFoundry,
			config: map[string]any{
				"projectEndpoint": "https://foundry.example/projects/p1",
				"auth": map[string]any{"oidc": map[string]any{
					"issuer": "https://login.microsoftonline.com", "clientId": "client",
					"clientSecretRef": map[string]any{"name": "credentials", "key": "clientSecret"},
				}},
			},
			wantErr: "must include a tenant path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runtime := Runtime{Metadata: ObjectMeta{Name: "runtime", Namespace: DefaultNamespace}, Spec: RuntimeSpec{Type: tt.runtimeType, Config: tt.config}}
			err := runtime.Validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func validMicrosoftAuth() map[string]any {
	return map[string]any{"oidc": map[string]any{
		"issuer": "https://login.microsoftonline.com/tenant/v2.0", "clientId": "client",
		"clientSecretRef": map[string]any{"name": "credentials", "key": "clientSecret"},
	}}
}
