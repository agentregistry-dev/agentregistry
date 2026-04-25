package local

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

func TestV1Alpha1Apply_MCPServerTarget_WritesComposeAndMarksProgressing(t *testing.T) {
	tmpDir := t.TempDir()

	originalUp := runLocalComposeUp
	originalDown := runLocalComposeDown
	t.Cleanup(func() {
		runLocalComposeUp = originalUp
		runLocalComposeDown = originalDown
	})
	var composeUpCalls int
	runLocalComposeUp = func(_ context.Context, dir string, _ bool) error {
		composeUpCalls++
		if dir != tmpDir {
			t.Fatalf("composeUp dir = %q, want %q", dir, tmpDir)
		}
		return nil
	}
	runLocalComposeDown = func(context.Context, string, bool) error { return nil }

	adapter := NewLocalDeploymentAdapter(tmpDir, 21212)

	target := &v1alpha1.MCPServer{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindMCPServer},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "weather", Version: "1.0.0"},
		Spec: v1alpha1.MCPServerSpec{
			Packages: []v1alpha1.MCPPackage{{
				RegistryType: "oci",
				Identifier:   "ghcr.io/example/weather:v1",
				Transport:    v1alpha1.MCPTransport{Type: "stdio"},
			}},
		},
	}
	deployment := &v1alpha1.Deployment{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindDeployment},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "weather-local", Version: "1", Generation: 7},
		Spec: v1alpha1.DeploymentSpec{
			TargetRef:    v1alpha1.ResourceRef{Kind: v1alpha1.KindMCPServer, Name: "weather", Version: "1.0.0"},
			ProviderRef:  v1alpha1.ResourceRef{Kind: v1alpha1.KindProvider, Name: "local", Version: "1"},
			DesiredState: v1alpha1.DesiredStateDeployed,
		},
	}
	provider := &v1alpha1.Provider{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindProvider},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "local", Version: "1"},
	}

	res, err := adapter.Apply(context.Background(), types.ApplyInput{
		Deployment: deployment,
		Target:     target,
		Provider:   provider,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if composeUpCalls != 1 {
		t.Fatalf("composeUp called %d times, want 1", composeUpCalls)
	}

	var gotProgressing *v1alpha1.Condition
	for i := range res.Conditions {
		if res.Conditions[i].Type == "Progressing" {
			gotProgressing = &res.Conditions[i]
			break
		}
	}
	if gotProgressing == nil {
		t.Fatalf("Progressing condition missing; got conditions = %+v", res.Conditions)
	}
	if gotProgressing.Status != v1alpha1.ConditionTrue {
		t.Fatalf("Progressing.Status = %q, want True", gotProgressing.Status)
	}
	if gotProgressing.ObservedGeneration != 7 {
		t.Fatalf("Progressing.ObservedGeneration = %d, want 7", gotProgressing.ObservedGeneration)
	}

	composePath := filepath.Join(tmpDir, "docker-compose.yaml")
	if _, err := os.Stat(composePath); err != nil {
		t.Fatalf("docker-compose.yaml not written: %v", err)
	}
	contents, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("read compose file: %v", err)
	}
	if !containsAll(string(contents), "ghcr.io/example/weather:v1", "agent_gateway") {
		t.Fatalf("compose file missing expected content:\n%s", contents)
	}
}

func TestV1Alpha1Remove_CallsComposeDown(t *testing.T) {
	tmpDir := t.TempDir()

	originalUp := runLocalComposeUp
	originalDown := runLocalComposeDown
	t.Cleanup(func() {
		runLocalComposeUp = originalUp
		runLocalComposeDown = originalDown
	})
	var downCalls int
	runLocalComposeUp = func(context.Context, string, bool) error { return nil }
	runLocalComposeDown = func(context.Context, string, bool) error {
		downCalls++
		return nil
	}

	adapter := NewLocalDeploymentAdapter(tmpDir, 21212)

	deployment := &v1alpha1.Deployment{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindDeployment},
		Metadata: v1alpha1.ObjectMeta{
			Namespace:  "default",
			Name:       "weather-local",
			Version:    "1",
			Generation: 3,
		},
	}
	res, err := adapter.Remove(context.Background(), types.RemoveInput{Deployment: deployment})
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(res.Conditions) == 0 || res.Conditions[0].Type != "Ready" {
		t.Fatalf("expected Ready condition; got %+v", res.Conditions)
	}
	if downCalls != 1 {
		t.Fatalf("composeDown calls = %d, want 1 (empty services post-remove should trigger down)", downCalls)
	}
}

func TestV1Alpha1SupportedTargetKinds(t *testing.T) {
	adapter := NewLocalDeploymentAdapter(t.TempDir(), 21212)
	kinds := adapter.SupportedTargetKinds()
	want := map[string]bool{v1alpha1.KindAgent: false, v1alpha1.KindMCPServer: false}
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

func TestV1Alpha1Logs_ReturnsClosedChannel(t *testing.T) {
	adapter := NewLocalDeploymentAdapter(t.TempDir(), 21212)
	ch, err := adapter.Logs(context.Background(), types.LogsInput{})
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	if _, open := <-ch; open {
		t.Fatalf("expected closed channel, got open channel with data")
	}
}

func containsAll(s string, needles ...string) bool {
	for _, n := range needles {
		if !contains(s, n) {
			return false
		}
	}
	return true
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
