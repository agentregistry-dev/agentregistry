package types

import (
	"context"
	"testing"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

func TestDefaultApplyFingerprintIgnoresStatusAndAnnotations(t *testing.T) {
	in := testApplyInput()

	first, err := DefaultApplyFingerprint(context.Background(), in, ApplyFingerprintOptions{AdapterType: "test"})
	if err != nil {
		t.Fatalf("DefaultApplyFingerprint: %v", err)
	}

	in.Deployment.Metadata.Annotations = map[string]string{"note": "changed"}
	in.Deployment.Status.SetCondition(v1alpha1.Condition{Type: "Ready", Status: v1alpha1.ConditionTrue})
	in.Target.GetMetadata().Annotations = map[string]string{"note": "changed"}
	in.Runtime.Status.SetCondition(v1alpha1.Condition{Type: "Ready", Status: v1alpha1.ConditionTrue})

	second, err := DefaultApplyFingerprint(context.Background(), in, ApplyFingerprintOptions{AdapterType: "test"})
	if err != nil {
		t.Fatalf("DefaultApplyFingerprint after status changes: %v", err)
	}
	if second != first {
		t.Fatalf("fingerprint changed after status/annotation-only edits: %s != %s", second, first)
	}
}

func TestDefaultApplyFingerprintIncludesAgentMCPServerDependency(t *testing.T) {
	in := testApplyInput()
	in.Target = &v1alpha1.Agent{
		TypeMeta: v1alpha1.TypeMeta{Kind: v1alpha1.KindAgent},
		Metadata: v1alpha1.ObjectMeta{
			Namespace:  "default",
			Name:       "assistant",
			UID:        "agent-uid",
			Generation: 1,
		},
		Spec: v1alpha1.AgentSpec{
			Title:      "assistant",
			MCPServers: []v1alpha1.ResourceRef{{Name: "weather"}},
		},
	}

	var identifier = "ghcr.io/example/weather:1.0.0"
	in.Getter = func(context.Context, v1alpha1.ResourceRef) (v1alpha1.Object, error) {
		return testMCPServer(identifier), nil
	}
	first, err := DefaultApplyFingerprint(context.Background(), in, ApplyFingerprintOptions{AdapterType: "test"})
	if err != nil {
		t.Fatalf("DefaultApplyFingerprint: %v", err)
	}

	identifier = "ghcr.io/example/weather:2.0.0"
	second, err := DefaultApplyFingerprint(context.Background(), in, ApplyFingerprintOptions{AdapterType: "test"})
	if err != nil {
		t.Fatalf("DefaultApplyFingerprint after dependency change: %v", err)
	}
	if second == first {
		t.Fatalf("fingerprint did not change after resolved MCPServer spec changed: %s", second)
	}
}

func testApplyInput() ApplyInput {
	return ApplyInput{
		Deployment: &v1alpha1.Deployment{
			TypeMeta: v1alpha1.TypeMeta{Kind: v1alpha1.KindDeployment},
			Metadata: v1alpha1.ObjectMeta{
				Namespace:  "default",
				Name:       "weather-deploy",
				UID:        "deployment-uid",
				Generation: 1,
			},
			Spec: v1alpha1.DeploymentSpec{
				TargetRef:  v1alpha1.ResourceRef{Kind: v1alpha1.KindMCPServer, Name: "weather"},
				RuntimeRef: v1alpha1.ResourceRef{Kind: v1alpha1.KindRuntime, Name: "local"},
				Env:        map[string]string{"LOG_LEVEL": "debug"},
			},
		},
		Target: testMCPServer("ghcr.io/example/weather:1.0.0"),
		Runtime: &v1alpha1.Runtime{
			TypeMeta: v1alpha1.TypeMeta{Kind: v1alpha1.KindRuntime},
			Metadata: v1alpha1.ObjectMeta{
				Namespace:  "default",
				Name:       "local",
				UID:        "runtime-uid",
				Generation: 1,
			},
			Spec: v1alpha1.RuntimeSpec{Type: "Local"},
		},
	}
}

func testMCPServer(identifier string) *v1alpha1.MCPServer {
	return &v1alpha1.MCPServer{
		TypeMeta: v1alpha1.TypeMeta{Kind: v1alpha1.KindMCPServer},
		Metadata: v1alpha1.ObjectMeta{
			Namespace:  "default",
			Name:       "weather",
			UID:        "mcp-uid",
			Generation: 1,
		},
		Spec: v1alpha1.MCPServerSpec{
			Source: &v1alpha1.MCPServerSource{
				Package: &v1alpha1.MCPPackage{
					RegistryType: v1alpha1.RegistryTypeOCI,
					Identifier:   identifier,
					Transport:    v1alpha1.MCPTransport{Type: "stdio"},
				},
			},
		},
	}
}
