package local

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

func TestV1Alpha1ApplyAndRemove_MCPServerTarget(t *testing.T) {
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
	var composeDownCalls int
	runLocalComposeDown = func(_ context.Context, dir string, _ bool) error {
		composeDownCalls++
		if dir != tmpDir {
			t.Fatalf("composeDown dir = %q, want %q", dir, tmpDir)
		}
		return nil
	}

	adapter := NewLocalDeploymentAdapter(tmpDir, 21212)
	in := localMCPApplyInput()

	res, err := adapter.Apply(context.Background(), in)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if composeUpCalls != 1 {
		t.Fatalf("composeUp called %d times, want 1", composeUpCalls)
	}

	var gotProgressing v1alpha1.Condition
	var foundProgressing bool
	for i := range res.Conditions {
		if res.Conditions[i].Type == "Progressing" {
			gotProgressing = res.Conditions[i]
			foundProgressing = true
			break
		}
	}
	if !foundProgressing {
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
	composeContents, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("read compose file: %v", err)
	}
	if !containsAll(string(composeContents), "ghcr.io/example/weather:v1", "agent_gateway") {
		t.Fatalf("compose file missing expected content:\n%s", composeContents)
	}

	gatewayPath := filepath.Join(tmpDir, "agent-gateway.yaml")
	gatewayContents, err := os.ReadFile(gatewayPath)
	if err != nil {
		t.Fatalf("read agent-gateway.yaml: %v", err)
	}
	if !containsAll(string(gatewayContents), "mcp_route", "weather-local") {
		t.Fatalf("agent-gateway.yaml missing expected target keyed to deployment:\n%s", gatewayContents)
	}

	removeRes, err := adapter.Remove(context.Background(), types.RemoveInput{Deployment: in.Deployment})
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(removeRes.Conditions) == 0 || removeRes.Conditions[0].Type != "Ready" {
		t.Fatalf("expected Ready condition; got %+v", removeRes.Conditions)
	}
	if composeUpCalls != 2 {
		t.Fatalf("composeUp calls after remove = %d, want 2", composeUpCalls)
	}
	if composeDownCalls != 0 {
		t.Fatalf("composeDown calls = %d, want 0 while agent_gateway remains", composeDownCalls)
	}

	gatewayContents, err = os.ReadFile(gatewayPath)
	if err != nil {
		t.Fatalf("read agent-gateway.yaml after remove: %v", err)
	}
	if strings.Contains(string(gatewayContents), "weather-local") {
		t.Fatalf("agent-gateway.yaml still contains removed deployment's target:\n%s", gatewayContents)
	}
}

func TestV1Alpha1Remove_EmptyRuntimeCallsComposeDown(t *testing.T) {
	originalUp := runLocalComposeUp
	originalDown := runLocalComposeDown
	t.Cleanup(func() {
		runLocalComposeUp = originalUp
		runLocalComposeDown = originalDown
	})
	runLocalComposeUp = func(context.Context, string, bool) error { return nil }
	var composeDownCalls int
	runLocalComposeDown = func(context.Context, string, bool) error {
		composeDownCalls++
		return nil
	}

	adapter := NewLocalDeploymentAdapter(t.TempDir(), 21212)
	res, err := adapter.Remove(context.Background(), types.RemoveInput{Deployment: &v1alpha1.Deployment{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindDeployment},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "weather-local"},
	}})
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(res.Conditions) == 0 || res.Conditions[0].Type != "Ready" {
		t.Fatalf("expected Ready condition; got %+v", res.Conditions)
	}
	if composeDownCalls != 1 {
		t.Fatalf("composeDown calls = %d, want 1", composeDownCalls)
	}
}

func localMCPApplyInput() types.ApplyInput {
	target := &v1alpha1.MCPServer{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindMCPServer},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "weather"},
		Spec: v1alpha1.MCPServerSpec{
			Source: &v1alpha1.MCPServerSource{
				Package: &v1alpha1.MCPPackage{
					Origin: v1alpha1.MCPPackageOrigin{
						Type:       v1alpha1.MCPPackageOriginTypeOCI,
						Identifier: "ghcr.io/example/weather:v1",
						OCI:        &v1alpha1.MCPPackageOriginOCI{ServerName: "weather"},
					},
					Transport: v1alpha1.MCPTransport{Type: "stdio"},
				},
			},
		},
	}
	deployment := &v1alpha1.Deployment{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindDeployment},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "weather-local", Generation: 7},
		Spec: v1alpha1.DeploymentSpec{
			TargetRef:    v1alpha1.ResourceRef{Kind: v1alpha1.KindMCPServer, Name: "weather"},
			RuntimeRef:   v1alpha1.ResourceRef{Kind: v1alpha1.KindRuntime, Name: "local"},
			DesiredState: v1alpha1.DesiredStateDeployed,
		},
	}
	runtime := &v1alpha1.Runtime{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindRuntime},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "local"},
	}
	return types.ApplyInput{Deployment: deployment, Target: target, Runtime: runtime}
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
		if !strings.Contains(s, n) {
			return false
		}
	}
	return true
}
