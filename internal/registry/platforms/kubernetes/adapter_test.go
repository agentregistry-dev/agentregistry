package kubernetes

import (
	"context"
	"testing"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	adapterpkgtypes "github.com/agentregistry-dev/agentregistry/pkg/types"
	v1alpha2 "github.com/kagent-dev/kagent/go/api/v1alpha2"
	kmcpv1alpha1 "github.com/kagent-dev/kmcp/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
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

	provider := &v1alpha1.Provider{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindProvider},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "kube-local", Version: "1"},
		Spec: v1alpha1.ProviderSpec{
			Platform: v1alpha1.PlatformKubernetes,
			Config:   map[string]any{"namespace": "kagent"},
		},
	}
	target := &v1alpha1.RemoteMCPServer{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindRemoteMCPServer},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "weather", Version: "1.0.0"},
		Spec: v1alpha1.RemoteMCPServerSpec{
			Remote: v1alpha1.MCPTransport{Type: "streamable-http", URL: "https://api.weather.example/mcp"},
		},
	}
	deployment := &v1alpha1.Deployment{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindDeployment},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "weather-kube", Version: "1", Generation: 4},
		Spec: v1alpha1.DeploymentSpec{
			TargetRef:    v1alpha1.ResourceRef{Kind: v1alpha1.KindRemoteMCPServer, Name: "weather", Version: "1.0.0"},
			ProviderRef:  v1alpha1.ResourceRef{Kind: v1alpha1.KindProvider, Name: "kube-local", Version: "1"},
			DesiredState: v1alpha1.DesiredStateDeployed,
		},
	}

	adapter := NewKubernetesDeploymentAdapter()
	res, err := adapter.Apply(context.Background(), adapterpkgtypes.ApplyInput{
		Deployment: deployment,
		Target:     target,
		Provider:   provider,
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

	provider := &v1alpha1.Provider{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "kube-local", Version: "1"},
		Spec:     v1alpha1.ProviderSpec{Platform: v1alpha1.PlatformKubernetes, Config: map[string]any{"namespace": "kagent"}},
	}
	deployment := &v1alpha1.Deployment{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: deploymentID, Version: "1", Generation: 5},
	}

	res, err := adapter.Remove(context.Background(), adapterpkgtypes.RemoveInput{
		Deployment: deployment,
		Provider:   provider,
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
		v1alpha1.KindAgent:           false,
		v1alpha1.KindMCPServer:       false,
		v1alpha1.KindRemoteMCPServer: false,
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

func TestK8sV1Alpha1Discover_SkipsManagedResources(t *testing.T) {
	unmanaged := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "imported", Namespace: "kagent"},
	}
	unmanagedRemote := &v1alpha2.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "imported-remote", Namespace: "kagent"},
	}
	managed := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "owned",
			Namespace: "kagent",
			Labels:    map[string]string{kubernetesManagedLabelKey: "true"},
		},
	}
	managedRemote := &v1alpha2.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "owned-remote",
			Namespace: "kagent",
			Labels:    map[string]string{kubernetesManagedLabelKey: "true"},
		},
	}
	withFakeKubeClient(t, unmanaged, unmanagedRemote, managed, managedRemote)

	adapter := NewKubernetesDeploymentAdapter()
	provider := &v1alpha1.Provider{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "kube-local", Version: "1"},
		Spec:     v1alpha1.ProviderSpec{Platform: v1alpha1.PlatformKubernetes, Config: map[string]any{"namespace": "kagent"}},
	}
	results, err := adapter.Discover(context.Background(), adapterpkgtypes.DiscoverInput{Provider: provider})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 unmanaged discoveries, got %d (%+v)", len(results), results)
	}
	byName := map[string]adapterpkgtypes.DiscoveryResult{}
	for _, result := range results {
		byName[result.Name] = result
	}
	if got := byName["imported"].TargetKind; got != v1alpha1.KindAgent {
		t.Fatalf("agent TargetKind = %q, want %q", got, v1alpha1.KindAgent)
	}
	if got := byName["imported-remote"].TargetKind; got != v1alpha1.KindRemoteMCPServer {
		t.Fatalf("remote TargetKind = %q, want %q", got, v1alpha1.KindRemoteMCPServer)
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
