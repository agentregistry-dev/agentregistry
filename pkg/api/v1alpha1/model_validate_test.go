package v1alpha1

import (
	"strings"
	"testing"
)

func TestModelValidate(t *testing.T) {
	secretRef := &SecretKeyRef{Name: "bedrock-key", Key: "api-key"}

	tests := []struct {
		name    string
		spec    ModelSpec
		wantErr string // substring; empty means valid
	}{
		{
			name: "valid bedrock runtime auth",
			spec: ModelSpec{Provider: "bedrock", Model: "us.anthropic.claude-opus-4-8", Auth: &ModelAuthConfig{Strategy: ModelAuthStrategyRuntime}},
		},
		{
			name: "valid bedrock omitted auth (provider default is runtime)",
			spec: ModelSpec{Provider: "bedrock", Model: "us.anthropic.claude-opus-4-8"},
		},
		{
			name: "valid bedrock secretRef auth",
			spec: ModelSpec{Provider: "bedrock", Model: "us.anthropic.claude-opus-4-8", Auth: &ModelAuthConfig{Strategy: ModelAuthStrategySecretRef, SecretRef: secretRef}},
		},
		{
			name: "valid bedrock passthrough auth with endpoint override",
			spec: ModelSpec{
				Provider: "bedrock", Model: "us.anthropic.claude-opus-4-8",
				Auth:     &ModelAuthConfig{Strategy: ModelAuthStrategyPassthrough},
				Endpoint: &ModelEndpointConfig{BaseURL: "https://litellm.dev.internal"},
			},
		},
		{
			name:    "provider must use canonical enum value",
			spec:    ModelSpec{Provider: "Bedrock", Model: "us.anthropic.claude-opus-4-8"},
			wantErr: "spec.provider",
		},
		{
			name:    "missing provider",
			spec:    ModelSpec{Model: "claude-opus-4-8"},
			wantErr: "spec.provider",
		},
		{
			name:    "unsupported anthropic provider",
			spec:    ModelSpec{Provider: "anthropic", Model: "claude-opus-4-8", Auth: &ModelAuthConfig{Strategy: ModelAuthStrategySecretRef, SecretRef: secretRef}},
			wantErr: "spec.provider",
		},
		{
			name:    "unsupported openai provider",
			spec:    ModelSpec{Provider: "openai", Model: "gpt-5", Auth: &ModelAuthConfig{Strategy: ModelAuthStrategySecretRef, SecretRef: secretRef}},
			wantErr: "spec.provider",
		},
		{
			name:    "unsupported vertex provider",
			spec:    ModelSpec{Provider: "vertex", Model: "gemini-2.5-pro", Auth: &ModelAuthConfig{Strategy: ModelAuthStrategyRuntime}},
			wantErr: "spec.provider",
		},
		{
			name:    "unknown provider",
			spec:    ModelSpec{Provider: "acme", Model: "m"},
			wantErr: "spec.provider",
		},
		{
			name:    "missing model",
			spec:    ModelSpec{Provider: "bedrock"},
			wantErr: "spec.model",
		},
		{
			name:    "secretRef strategy without secretRef",
			spec:    ModelSpec{Provider: "bedrock", Model: "us.anthropic.claude-opus-4-8", Auth: &ModelAuthConfig{Strategy: ModelAuthStrategySecretRef}},
			wantErr: "spec.auth.secretRef",
		},
		{
			name:    "runtime strategy with stray secretRef",
			spec:    ModelSpec{Provider: "bedrock", Model: "m", Auth: &ModelAuthConfig{Strategy: ModelAuthStrategyRuntime, SecretRef: secretRef}},
			wantErr: "spec.auth.secretRef",
		},
		{
			name:    "unknown auth strategy",
			spec:    ModelSpec{Provider: "bedrock", Model: "m", Auth: &ModelAuthConfig{Strategy: "oauth"}},
			wantErr: "spec.auth.strategy",
		},
		{
			name:    "auth strategy must use canonical enum value",
			spec:    ModelSpec{Provider: "bedrock", Model: "m", Auth: &ModelAuthConfig{Strategy: " runtime "}},
			wantErr: "spec.auth.strategy",
		},
		{
			name:    "empty auth strategy on non-nil auth",
			spec:    ModelSpec{Provider: "bedrock", Model: "m", Auth: &ModelAuthConfig{}},
			wantErr: "spec.auth.strategy",
		},
		{
			name:    "secretRef with invalid name",
			spec:    ModelSpec{Provider: "bedrock", Model: "m", Auth: &ModelAuthConfig{Strategy: ModelAuthStrategySecretRef, SecretRef: &SecretKeyRef{Name: "Not A Name!"}}},
			wantErr: "spec.auth.secretRef.name",
		},
		{
			name: "tls caCert secretRef validated",
			spec: ModelSpec{
				Provider: "bedrock", Model: "m",
				Endpoint: &ModelEndpointConfig{TLS: &ModelTLSConfig{CACertSecretRef: &SecretKeyRef{Name: "bad name!"}}},
			},
			wantErr: "spec.endpoint.tls.caCertSecretRef.name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Model{
				TypeMeta: TypeMeta{APIVersion: GroupVersion, Kind: KindModel},
				Metadata: ObjectMeta{Namespace: "default", Name: "my-model"},
				Spec:     tt.spec,
			}
			err := m.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}
