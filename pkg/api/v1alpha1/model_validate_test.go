package v1alpha1

import (
	"strings"
	"testing"
)

func TestModelValidate(t *testing.T) {
	secretRef := &SecretKeyRef{Name: "anthropic-key", Key: "api-key"}

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
			name: "valid anthropic secretRef auth",
			spec: ModelSpec{Provider: "anthropic", Model: "claude-opus-4-8", Auth: &ModelAuthConfig{Strategy: ModelAuthStrategySecretRef, SecretRef: secretRef}},
		},
		{
			name: "valid anthropic passthrough auth with endpoint override",
			spec: ModelSpec{
				Provider: "anthropic", Model: "claude-opus-4-8",
				Auth:     &ModelAuthConfig{Strategy: ModelAuthStrategyPassthrough},
				Endpoint: &ModelEndpointConfig{BaseURL: "https://litellm.dev.internal"},
			},
		},
		{
			name: "provider canonicalized to lowercase",
			spec: ModelSpec{Provider: "Bedrock", Model: "us.anthropic.claude-opus-4-8"},
		},
		{
			name:    "missing provider",
			spec:    ModelSpec{Model: "claude-opus-4-8"},
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
			name:    "key-based provider with omitted auth",
			spec:    ModelSpec{Provider: "anthropic", Model: "claude-opus-4-8"},
			wantErr: "spec.auth",
		},
		{
			name:    "key-based provider with runtime auth",
			spec:    ModelSpec{Provider: "openai", Model: "gpt-5", Auth: &ModelAuthConfig{Strategy: ModelAuthStrategyRuntime}},
			wantErr: "spec.auth.strategy",
		},
		{
			name:    "secretRef strategy without secretRef",
			spec:    ModelSpec{Provider: "anthropic", Model: "claude-opus-4-8", Auth: &ModelAuthConfig{Strategy: ModelAuthStrategySecretRef}},
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
			name:    "empty auth strategy on non-nil auth",
			spec:    ModelSpec{Provider: "bedrock", Model: "m", Auth: &ModelAuthConfig{}},
			wantErr: "spec.auth.strategy",
		},
		{
			name:    "secretRef with invalid name",
			spec:    ModelSpec{Provider: "anthropic", Model: "m", Auth: &ModelAuthConfig{Strategy: ModelAuthStrategySecretRef, SecretRef: &SecretKeyRef{Name: "Not A Name!"}}},
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

func TestModelValidateCanonicalizesProvider(t *testing.T) {
	m := &Model{
		Metadata: ObjectMeta{Namespace: "default", Name: "m"},
		Spec:     ModelSpec{Provider: "  Bedrock ", Model: "us.anthropic.claude-opus-4-8"},
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("Validate() unexpected error: %v", err)
	}
	if m.Spec.Provider != ModelProviderBedrock {
		t.Fatalf("Validate() provider = %q, want %q", m.Spec.Provider, ModelProviderBedrock)
	}
}

func TestDeploymentValidateModelRef(t *testing.T) {
	base := func() DeploymentSpec {
		return DeploymentSpec{
			TargetRef:  ResourceRef{Kind: KindAgent, Name: "my-agent"},
			RuntimeRef: ResourceRef{Kind: KindRuntime, Name: "local"},
		}
	}

	t.Run("harness deployment requires modelRef", func(t *testing.T) {
		spec := base()
		spec.Harness = &DeploymentHarness{Type: "claude-code"}
		d := &Deployment{Metadata: ObjectMeta{Namespace: "default", Name: "d"}, Spec: spec}
		err := d.Validate()
		if err == nil || !strings.Contains(err.Error(), "spec.modelRef") {
			t.Fatalf("Validate() = %v, want spec.modelRef required error", err)
		}
	})

	t.Run("harness deployment with modelRef is valid", func(t *testing.T) {
		spec := base()
		spec.Harness = &DeploymentHarness{Type: "claude-code"}
		spec.ModelRef = &ModelRef{Name: "claude-opus-4-8"}
		d := &Deployment{Metadata: ObjectMeta{Namespace: "default", Name: "d"}, Spec: spec}
		if err := d.Validate(); err != nil {
			t.Fatalf("Validate() unexpected error: %v", err)
		}
	})

	t.Run("non-harness deployment does not require modelRef", func(t *testing.T) {
		d := &Deployment{Metadata: ObjectMeta{Namespace: "default", Name: "d"}, Spec: base()}
		if err := d.Validate(); err != nil {
			t.Fatalf("Validate() unexpected error: %v", err)
		}
	})

	t.Run("modelRef name validated", func(t *testing.T) {
		spec := base()
		spec.ModelRef = &ModelRef{Name: "Not Valid!"}
		d := &Deployment{Metadata: ObjectMeta{Namespace: "default", Name: "d"}, Spec: spec}
		err := d.Validate()
		if err == nil || !strings.Contains(err.Error(), "spec.modelRef.name") {
			t.Fatalf("Validate() = %v, want spec.modelRef.name error", err)
		}
	})

	t.Run("modelRef namespace validated", func(t *testing.T) {
		spec := base()
		spec.ModelRef = &ModelRef{Namespace: "Bad Namespace!", Name: "m"}
		d := &Deployment{Metadata: ObjectMeta{Namespace: "default", Name: "d"}, Spec: spec}
		err := d.Validate()
		if err == nil || !strings.Contains(err.Error(), "spec.modelRef.namespace") {
			t.Fatalf("Validate() = %v, want spec.modelRef.namespace error", err)
		}
	})
}
