package kagent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

func TestDecodeRuntimeConfig(t *testing.T) {
	tests := []struct {
		name    string
		in      map[string]any
		want    runtimeConfig
		wantErr string
	}{
		{
			name: "full config",
			in: map[string]any{
				"kagentUrl": "http://kagent.kagent.svc:8083", "namespace": "kagent",
				"auth": map[string]any{
					"secretRef": map[string]any{"name": "kagent", "key": "token"},
					"userID":    "ar",
				},
				"imagePullSecrets": []any{"registry-creds"},
				"deployment": map[string]any{
					"nodeSelector": map[string]any{"node-group": "agents"},
					"tolerations": []any{map[string]any{
						"key": "dedicated", "operator": "Equal", "value": "agents", "effect": "NoSchedule",
					}},
					"affinity": map[string]any{
						"nodeAffinity": map[string]any{},
					},
				},
				"unknown": "ignored",
			},
			want: runtimeConfig{
				URL:       "http://kagent.kagent.svc:8083",
				Namespace: "kagent",
				Auth: authConfig{
					SecretRef: &v1alpha1.SecretRef{Name: "kagent", Key: "token"},
					UserID:    "ar",
				},
				ImagePullSecrets: []string{"registry-creds"},
				Deployment: runtimeDeploymentConfig{
					NodeSelector: map[string]string{"node-group": "agents"},
					Tolerations: []corev1.Toleration{{
						Key: "dedicated", Operator: corev1.TolerationOpEqual, Value: "agents", Effect: corev1.TaintEffectNoSchedule,
					}},
					Affinity: &corev1.Affinity{NodeAffinity: &corev1.NodeAffinity{}},
				},
			},
		},
		{name: "missing kagentUrl", in: map[string]any{"namespace": "x"}, wantErr: "kagentUrl is required"},
		{name: "bad namespace", in: map[string]any{"kagentUrl": "http://k", "namespace": "Bad_NS"}, wantErr: "namespace"},
		{name: "raw token rejected", in: map[string]any{"kagentUrl": "http://k", "auth": map[string]any{"token": "plaintext"}}, wantErr: "auth.token is not supported"},
		{name: "case variant raw token rejected", in: map[string]any{"kagentUrl": "http://k", "Auth": map[string]any{"Token": "plaintext"}}, wantErr: "auth.token is not supported"},
		{name: "secret key required", in: map[string]any{"kagentUrl": "http://k", "auth": map[string]any{"secretRef": map[string]any{"name": "kagent"}}}, wantErr: "secretRef.key is required"},
		{name: "secret namespace rejected", in: map[string]any{"kagentUrl": "http://k", "auth": map[string]any{"secretRef": map[string]any{"namespace": "other", "name": "kagent", "key": "token"}}}, wantErr: "secretRef.namespace is not supported"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeRuntimeConfig(tt.in)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDecodeRuntimeConnectionConfigIgnoresWorkloadFields(t *testing.T) {
	got, err := decodeRuntimeConnectionConfig(map[string]any{
		"kagentUrl":        "http://kagent.kagent.svc:8083",
		"namespace":        "kagent",
		"imagePullSecrets": []any{123},
		"deployment":       map[string]any{"nodeSelector": "invalid"},
	})
	require.NoError(t, err)
	assert.Equal(t, runtimeConfig{
		URL:       "http://kagent.kagent.svc:8083",
		Namespace: "kagent",
	}, got)
}

func TestDecodeRuntimeConnectionConfigRejectsCaseVariantRawToken(t *testing.T) {
	_, err := decodeRuntimeConnectionConfig(map[string]any{
		"kagentUrl": "http://kagent.kagent.svc:8083",
		"Auth":      map[string]any{"Token": "plaintext"},
	})
	require.ErrorContains(t, err, "auth.token is not supported")
}

func TestDecodeRuntimeURL(t *testing.T) {
	decoders := map[string]func(map[string]any) (runtimeConfig, error){
		"deployment": decodeRuntimeConfig,
		"connection": decodeRuntimeConnectionConfig,
	}
	tests := []struct {
		name    string
		url     string
		wantURL string
		wantErr string
	}{
		{name: "malformed", url: "://kagent", wantErr: "kagentUrl must be a valid URL"},
		{name: "unsupported scheme", url: "ftp://kagent.example.com", wantErr: "kagentUrl must use http or https"},
		{name: "missing host", url: "https:///api", wantErr: "kagentUrl must include a host"},
		{name: "missing hostname", url: "https://:8443/api", wantErr: "kagentUrl must include a host"},
		{name: "query string", url: "https://kagent.example.com/api?tenant=team-a", wantErr: "kagentUrl must not include a query string"},
		{name: "fragment", url: "https://kagent.example.com/api#agents", wantErr: "kagentUrl must not include a fragment"},
		{name: "surrounding whitespace", url: "  https://kagent.example.com/api  ", wantURL: "https://kagent.example.com/api"},
	}

	for decoderName, decode := range decoders {
		for _, tt := range tests {
			t.Run(decoderName+"/"+tt.name, func(t *testing.T) {
				got, err := decode(map[string]any{"kagentUrl": tt.url})
				if tt.wantErr != "" {
					require.ErrorContains(t, err, tt.wantErr)
					return
				}
				require.NoError(t, err)
				assert.Equal(t, tt.wantURL, got.URL)
			})
		}
	}
}

func TestRuntimeTypeRegisteredForAdmission(t *testing.T) {
	rt := &v1alpha1.Runtime{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "r"},
		Spec: v1alpha1.RuntimeSpec{
			Type:   "kagent", // case-insensitive input
			Config: map[string]any{"kagentUrl": "https://kagent.example.com"},
		},
	}
	require.NoError(t, rt.Validate())
	assert.Equal(t, RuntimeType, rt.Spec.Type) // canonicalized to "Kagent"
}

func TestRuntimeAdmissionRejectsRawKagentToken(t *testing.T) {
	runtime := &v1alpha1.Runtime{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "r"},
		Spec: v1alpha1.RuntimeSpec{Type: RuntimeType, Config: map[string]any{
			"kagentUrl": "https://kagent.example.com",
			"auth":      map[string]any{"token": "plaintext"},
		}},
	}

	require.ErrorContains(t, runtime.Validate(), "auth.token is not supported")
}

func TestRuntimeAdmissionRejectsRawKagentTokenFromStringMap(t *testing.T) {
	runtime := &v1alpha1.Runtime{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "r"},
		Spec: v1alpha1.RuntimeSpec{Type: RuntimeType, Config: map[string]any{
			"kagentUrl": "https://kagent.example.com",
			"auth":      map[string]string{"token": "plaintext"},
		}},
	}

	require.ErrorContains(t, runtime.Validate(), "auth.token is not supported")
}

func TestRuntimeAdmissionRejectsCaseVariantRawKagentToken(t *testing.T) {
	runtime := &v1alpha1.Runtime{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "r"},
		Spec: v1alpha1.RuntimeSpec{Type: RuntimeType, Config: map[string]any{
			"kagentUrl": "https://kagent.example.com",
			"Auth":      map[string]any{"Token": "plaintext"},
		}},
	}

	require.ErrorContains(t, runtime.Validate(), "auth.token is not supported")
}

func TestDecodeDeployConfig(t *testing.T) {
	tests := []struct {
		name    string
		in      map[string]any
		kind    string
		want    deployConfig
		wantErr string
	}{
		{name: "mcp secretRefs", in: map[string]any{"namespace": "team-a", "secretRefs": []any{"s1"}}, kind: v1alpha1.KindMCPServer,
			want: deployConfig{SecretRefs: []string{"s1"}}},
		{name: "agent rejects secretRefs", in: map[string]any{"secretRefs": []any{"s1"}}, kind: v1alpha1.KindAgent,
			wantErr: "secretRefs is only supported for MCPServer"},
		{name: "agent rejects case variant secretRefs", in: map[string]any{"SecretRefs": []any{"s1"}}, kind: v1alpha1.KindAgent,
			wantErr: "secretRefs is only supported for MCPServer"},
		{name: "invalid secret name", in: map[string]any{"secretRefs": []any{"Bad_Name"}}, kind: v1alpha1.KindMCPServer,
			wantErr: "not a valid Kubernetes Secret name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeDeployConfig(tt.in, tt.kind)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
