package kubernetes

import (
	"context"
	"reflect"
	"testing"

	v1alpha2 "github.com/kagent-dev/kagent/go/api/v1alpha2"
	kmcpv1alpha1 "github.com/kagent-dev/kmcp/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	adapterpkgtypes "github.com/agentregistry-dev/agentregistry/pkg/types"
)

func withFakeKubeClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	fakeClient := fake.NewClientBuilder().WithScheme(kubernetesScheme).WithObjects(objs...).Build()
	originalAmbientRESTConfig := kubernetesGetAmbientRESTConfig
	originalNewClientForConfig := kubernetesNewClientForConfig
	t.Cleanup(func() {
		kubernetesGetAmbientRESTConfig = originalAmbientRESTConfig
		kubernetesNewClientForConfig = originalNewClientForConfig
	})
	kubernetesGetAmbientRESTConfig = func() (*rest.Config, error) {
		return &rest.Config{Host: "https://fake.test"}, nil
	}
	kubernetesNewClientForConfig = func(*rest.Config) (client.Client, error) {
		return fakeClient, nil
	}
	return fakeClient
}

func TestK8sV1Alpha1Apply_MCPServerTarget_CreatesResource(t *testing.T) {
	fakeClient := withFakeKubeClient(t)

	runtime := &v1alpha1.Runtime{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindRuntime},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "kube-local"},
		Spec: v1alpha1.RuntimeSpec{
			Type:   v1alpha1.TypeKubernetes,
			Config: map[string]any{"namespace": "kagent"},
		},
	}
	target := &v1alpha1.MCPServer{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindMCPServer},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "weather"},
		Spec: v1alpha1.MCPServerSpec{
			Remote: &v1alpha1.MCPRemote{Type: "streamable-http", URL: "https://api.weather.example/mcp"},
		},
	}
	deployment := &v1alpha1.Deployment{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindDeployment},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "weather-kube", Generation: 4},
		Spec: v1alpha1.DeploymentSpec{
			TargetRef:    v1alpha1.ResourceRef{Kind: v1alpha1.KindMCPServer, Name: "weather"},
			RuntimeRef:   v1alpha1.ResourceRef{Kind: v1alpha1.KindRuntime, Name: "kube-local"},
			DesiredState: v1alpha1.DesiredStateDeployed,
		},
	}

	adapter := NewKubernetesDeploymentAdapter()
	res, err := adapter.Apply(context.Background(), adapterpkgtypes.ApplyInput{
		Deployment: deployment,
		Target:     target,
		Runtime:    runtime,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	var progressing *v1alpha1.Condition
	for i := range res.Conditions {
		if res.Conditions[i].Type == "Progressing" {
			progressing = &res.Conditions[i]
		}
	}
	if progressing == nil || progressing.Status != v1alpha1.ConditionTrue || progressing.ObservedGeneration != 4 {
		t.Fatalf("Progressing condition unexpected: %+v", progressing)
	}

	// Verify the RemoteMCPServer resource was created in the kagent namespace.
	remoteMCPs := &v1alpha2.RemoteMCPServerList{}
	if err := fakeClient.List(context.Background(), remoteMCPs); err != nil {
		t.Fatalf("list RemoteMCPServers: %v", err)
	}
	if len(remoteMCPs.Items) != 1 {
		t.Fatalf("expected 1 RemoteMCPServer, got %d", len(remoteMCPs.Items))
	}
	if remoteMCPs.Items[0].Namespace != "kagent" {
		t.Fatalf("RemoteMCPServer namespace = %q, want kagent", remoteMCPs.Items[0].Namespace)
	}
}

func TestBuildDesiredState_AgentUsesResolvedModel(t *testing.T) {
	adapter := NewKubernetesDeploymentAdapter()
	deployment := &v1alpha1.Deployment{
		Metadata: v1alpha1.ObjectMeta{Namespace: "team-a", Name: "assistant-kube"},
		Spec: v1alpha1.DeploymentSpec{
			ModelRef: &v1alpha1.ModelRef{Name: "approved-model"},
			Env: map[string]string{
				"MODEL_PROVIDER": "deployment-provider",
				"MODEL_NAME":     "deployment-model",
			},
		},
	}
	target := &v1alpha1.Agent{
		Metadata: v1alpha1.ObjectMeta{Namespace: "team-a", Name: "assistant", Tag: "stable"},
		Spec:     v1alpha1.AgentSpec{Source: &v1alpha1.AgentSource{Image: "ghcr.io/example/assistant:v1"}},
	}
	var gotRef v1alpha1.ResourceRef
	desired, err := adapter.buildDesiredStateFromV1Alpha1(t.Context(), adapterpkgtypes.ApplyInput{
		Deployment: deployment,
		Target:     target,
		Getter: func(_ context.Context, ref v1alpha1.ResourceRef) (v1alpha1.Object, error) {
			gotRef = ref
			return &v1alpha1.Model{
				Spec: v1alpha1.ModelSpec{
					Provider: v1alpha1.ModelProviderBedrock,
					Model:    "us.anthropic.claude-sonnet-4-6",
				},
			}, nil
		},
	}, "kagent")
	if err != nil {
		t.Fatalf("buildDesiredStateFromV1Alpha1: %v", err)
	}
	if gotRef.Namespace != "team-a" || gotRef.Name != "approved-model" || gotRef.Tag != "latest" {
		t.Fatalf("resolved ref = %+v", gotRef)
	}
	if len(desired.Agents) != 1 {
		t.Fatalf("agents = %+v", desired.Agents)
	}
	env := desired.Agents[0].Deployment.Env
	if env["MODEL_PROVIDER"] != v1alpha1.ModelProviderBedrock ||
		env["MODEL_NAME"] != "us.anthropic.claude-sonnet-4-6" {
		t.Fatalf("model env = %+v", env)
	}
}

func TestK8sV1Alpha1Remove_DeletesResourcesByDeploymentID(t *testing.T) {
	// Seed the fake client with an Agent + MCPServer labeled for our deployment.
	deploymentID := "weather-kube"
	managedLabels := map[string]string{
		kubernetesManagedLabelKey:      "true",
		kubernetesDeploymentIDLabelKey: deploymentID,
	}
	seedAgent := &v1alpha2.Agent{
		TypeMeta:   metav1.TypeMeta{APIVersion: "kagent.dev/v1alpha2", Kind: "Agent"},
		ObjectMeta: metav1.ObjectMeta{Name: "legacy-agent", Namespace: "kagent", Labels: managedLabels},
	}
	seedMCP := &kmcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "legacy-mcp", Namespace: "kagent", Labels: managedLabels},
	}
	fakeClient := withFakeKubeClient(t, seedAgent, seedMCP)

	adapter := NewKubernetesDeploymentAdapter()

	runtime := &v1alpha1.Runtime{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "kube-local"},
		Spec:     v1alpha1.RuntimeSpec{Type: v1alpha1.TypeKubernetes, Config: map[string]any{"namespace": "kagent"}},
	}
	deployment := &v1alpha1.Deployment{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: deploymentID, Generation: 5},
	}

	res, err := adapter.Remove(context.Background(), adapterpkgtypes.RemoveInput{
		Deployment: deployment,
		Runtime:    runtime,
	})
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(res.Conditions) == 0 {
		t.Fatalf("expected at least one condition; got %+v", res.Conditions)
	}

	// Both seed resources should be gone.
	gotAgent := &v1alpha2.Agent{}
	err = fakeClient.Get(context.Background(), k8stypes.NamespacedName{Name: "legacy-agent", Namespace: "kagent"}, gotAgent)
	if err == nil {
		t.Fatalf("Agent should have been deleted, still found %s", gotAgent.Name)
	}
	gotMCP := &kmcpv1alpha1.MCPServer{}
	err = fakeClient.Get(context.Background(), k8stypes.NamespacedName{Name: "legacy-mcp", Namespace: "kagent"}, gotMCP)
	if err == nil {
		t.Fatalf("MCPServer should have been deleted, still found %s", gotMCP.Name)
	}
}

func TestK8sV1Alpha1SupportedTargetKinds(t *testing.T) {
	adapter := NewKubernetesDeploymentAdapter()
	kinds := adapter.SupportedTargetKinds()
	want := map[string]bool{
		v1alpha1.KindAgent:     false,
		v1alpha1.KindMCPServer: false,
	}
	for _, k := range kinds {
		if _, ok := want[k]; ok {
			want[k] = true
		}
	}
	for k, seen := range want {
		if !seen {
			t.Fatalf("missing supported kind %q; got %v", k, kinds)
		}
	}
}

func TestK8sV1Alpha1Logs_ReturnsClosedChannel(t *testing.T) {
	adapter := NewKubernetesDeploymentAdapter()
	ch, err := adapter.Logs(context.Background(), adapterpkgtypes.LogsInput{})
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	if _, open := <-ch; open {
		t.Fatalf("expected closed channel")
	}
}

// TestK8sV1Alpha1Apply_BundledMCPServerEnvFrom_CreatesResourceWithSecretRefs
// walks the adapter's full apply path for a bundled MCPServer Deployment
// carrying spec.envFrom and asserts the created kmcp MCPServer resource
// carries spec.deployment.secretRefs (which the kmcp controller renders as
// container envFrom).
func TestK8sV1Alpha1Apply_BundledMCPServerEnvFrom_CreatesResourceWithSecretRefs(t *testing.T) {
	fakeClient := withFakeKubeClient(t)

	runtime := &v1alpha1.Runtime{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindRuntime},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "kube-local"},
		Spec: v1alpha1.RuntimeSpec{
			Type:   v1alpha1.TypeKubernetes,
			Config: map[string]any{"namespace": "kagent"},
		},
	}
	target := &v1alpha1.MCPServer{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindMCPServer},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "weather"},
		Spec: v1alpha1.MCPServerSpec{
			Source: &v1alpha1.MCPServerSource{
				Package: &v1alpha1.MCPPackage{
					Origin: v1alpha1.MCPPackageOrigin{
						Type:       v1alpha1.MCPPackageOriginTypeOCI,
						Identifier: "docker.io/example/weather:1.0",
						OCI:        &v1alpha1.MCPPackageOriginOCI{ServerName: "io.example/weather"},
					},
					Transport: v1alpha1.MCPTransport{Type: "stdio"},
					Launch:    &v1alpha1.MCPPackageLaunch{Command: "python"},
				},
			},
		},
	}
	deployment := &v1alpha1.Deployment{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindDeployment},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "weather-kube"},
		Spec: v1alpha1.DeploymentSpec{
			TargetRef:    v1alpha1.ResourceRef{Kind: v1alpha1.KindMCPServer, Name: "weather"},
			RuntimeRef:   v1alpha1.ResourceRef{Kind: v1alpha1.KindRuntime, Name: "kube-local"},
			DesiredState: v1alpha1.DesiredStateDeployed,
			Env:          map[string]string{"env": "fromenv"},
			EnvFrom: []v1alpha1.EnvFromSource{
				{SecretRef: &v1alpha1.SecretEnvSource{Name: "mcp-secrets"}},
			},
		},
	}

	adapter := NewKubernetesDeploymentAdapter()
	if _, err := adapter.Apply(context.Background(), adapterpkgtypes.ApplyInput{
		Deployment: deployment,
		Target:     target,
		Runtime:    runtime,
	}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	mcps := &kmcpv1alpha1.MCPServerList{}
	if err := fakeClient.List(context.Background(), mcps); err != nil {
		t.Fatalf("list MCPServers: %v", err)
	}
	if len(mcps.Items) != 1 {
		t.Fatalf("expected 1 MCPServer, got %d", len(mcps.Items))
	}
	dep := mcps.Items[0].Spec.Deployment
	wantRefs := []corev1.LocalObjectReference{{Name: "mcp-secrets"}}
	if !reflect.DeepEqual(dep.SecretRefs, wantRefs) {
		t.Fatalf("secretRefs = %+v, want %+v", dep.SecretRefs, wantRefs)
	}
	if dep.Env["env"] != "fromenv" {
		t.Fatalf("env env = %q, want fromenv (explicit env forwarded beside the refs)", dep.Env["env"])
	}
}
