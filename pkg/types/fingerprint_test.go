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

func TestDefaultApplyFingerprintResultIncludesDependencySnapshot(t *testing.T) {
	in := testApplyInput()
	in.Target = &v1alpha1.Agent{
		TypeMeta: v1alpha1.TypeMeta{Kind: v1alpha1.KindAgent},
		Metadata: v1alpha1.ObjectMeta{
			Namespace: "default",
			Name:      "assistant",
		},
		Spec: v1alpha1.AgentSpec{
			MCPServers: []v1alpha1.ResourceRef{{Name: "weather"}},
		},
	}

	in.Getter = func(context.Context, v1alpha1.ResourceRef) (v1alpha1.Object, error) {
		return testMCPServer("ghcr.io/example/weather:1.0.0"), nil
	}

	result, err := DefaultApplyFingerprintResult(context.Background(), in, ApplyFingerprintOptions{AdapterType: "test"})
	if err != nil {
		t.Fatalf("DefaultApplyFingerprintResult: %v", err)
	}
	if result.Fingerprint == "" {
		t.Fatalf("fingerprint is empty")
	}
	if len(result.Dependencies) != 1 {
		t.Fatalf("dependencies = %+v, want one MCPServer dependency", result.Dependencies)
	}
	dep := result.Dependencies[0]
	if dep.Kind != v1alpha1.KindMCPServer || dep.Namespace != "default" || dep.Name != "weather" {
		t.Fatalf("dependency identity mismatch: %+v", dep)
	}
	if dep.UID != "mcp-uid" || dep.Generation != 1 {
		t.Fatalf("dependency version mismatch: %+v", dep)
	}
	if dep.MaterialHash == "" {
		t.Fatalf("dependency material hash is empty: %+v", dep)
	}
}

func TestDefaultApplyFingerprintResultIncludesModelDependency(t *testing.T) {
	in := testApplyInput()
	in.Deployment.Metadata.Namespace = "team-a"
	in.Deployment.Spec.ModelRef = &v1alpha1.ModelRef{Name: "approved-model"}

	modelID := "us.anthropic.claude-sonnet-4-6"
	in.Getter = func(_ context.Context, ref v1alpha1.ResourceRef) (v1alpha1.Object, error) {
		if ref.Kind != v1alpha1.KindModel || ref.Namespace != "team-a" || ref.Name != "approved-model" || ref.Tag != "" {
			t.Fatalf("model ref = %+v, want team-a/approved-model with implicit latest tag", ref)
		}
		return testModel("team-a", "approved-model", "latest", modelID), nil
	}

	first, err := DefaultApplyFingerprintResult(context.Background(), in, ApplyFingerprintOptions{AdapterType: "test"})
	if err != nil {
		t.Fatalf("DefaultApplyFingerprintResult: %v", err)
	}
	if len(first.Dependencies) != 1 {
		t.Fatalf("dependencies = %+v, want one Model dependency", first.Dependencies)
	}
	dep := first.Dependencies[0]
	if dep.Kind != v1alpha1.KindModel || dep.Namespace != "team-a" || dep.Name != "approved-model" || dep.Tag != "latest" {
		t.Fatalf("model dependency identity mismatch: %+v", dep)
	}
	if dep.UID != "model-uid" || dep.Generation != 1 || dep.MaterialHash == "" {
		t.Fatalf("model dependency version mismatch: %+v", dep)
	}

	modelID = "us.anthropic.claude-opus-4-8"
	second, err := DefaultApplyFingerprintResult(context.Background(), in, ApplyFingerprintOptions{AdapterType: "test"})
	if err != nil {
		t.Fatalf("DefaultApplyFingerprintResult after Model change: %v", err)
	}
	if second.Fingerprint == first.Fingerprint {
		t.Fatalf("fingerprint did not change after resolved Model spec changed: %s", second.Fingerprint)
	}
}

func TestDefaultApplyFingerprintIncludesAgentHarnessCompositionDependencies(t *testing.T) {
	in := testApplyInput()
	in.Deployment.Metadata.Namespace = "team-a"
	in.Deployment.Spec.Harness = &v1alpha1.DeploymentHarness{Type: "claude-code"}
	in.Deployment.Spec.ModelRef = &v1alpha1.ModelRef{Name: "approved-model"}
	in.Target = &v1alpha1.Agent{
		TypeMeta: v1alpha1.TypeMeta{Kind: v1alpha1.KindAgent},
		Metadata: v1alpha1.ObjectMeta{
			Namespace: "team-a",
			Name:      "assistant",
		},
		Spec: v1alpha1.AgentSpec{
			CompatibleHarnesses: []v1alpha1.HarnessCompatibility{{Type: "claude-code"}},
			Plugins: []v1alpha1.ResourceRef{{
				Name: "deploy-tools",
			}},
			Skills: []v1alpha1.ResourceRef{{
				Name: "weather",
			}},
			Instructions: &v1alpha1.ResourceRef{Name: "writer-instructions"},
			MCPServers: []v1alpha1.ResourceRef{{
				Name: "search",
			}},
		},
	}

	pluginCommit := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	skillCommit := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	in.Getter = func(_ context.Context, ref v1alpha1.ResourceRef) (v1alpha1.Object, error) {
		switch ref.Kind {
		case v1alpha1.KindPlugin:
			return testPlugin(ref.Namespace, ref.Name, pluginCommit), nil
		case v1alpha1.KindSkill:
			return testSkill(ref.Namespace, ref.Name, skillCommit), nil
		case v1alpha1.KindPrompt:
			return testPrompt(ref.Namespace, ref.Name, "Write concise rollout notes."), nil
		case v1alpha1.KindMCPServer:
			return testMCPServerInNamespace(ref.Namespace, ref.Name, "ghcr.io/example/search:1.0.0"), nil
		case v1alpha1.KindModel:
			return testModel(ref.Namespace, ref.Name, "latest", "us.anthropic.claude-sonnet-4-6"), nil
		default:
			t.Fatalf("unexpected dependency ref: %+v", ref)
			return nil, nil
		}
	}

	first, err := DefaultApplyFingerprintResult(context.Background(), in, ApplyFingerprintOptions{AdapterType: "test"})
	if err != nil {
		t.Fatalf("DefaultApplyFingerprintResult: %v", err)
	}
	if len(first.Dependencies) != 5 {
		t.Fatalf("dependencies = %+v, want Model, Plugin, Skill, Prompt, and MCPServer", first.Dependencies)
	}
	for _, dep := range first.Dependencies {
		if dep.Namespace != "team-a" {
			t.Fatalf("dependency namespace = %q, want team-a: %+v", dep.Namespace, dep)
		}
		if dep.MaterialHash == "" {
			t.Fatalf("dependency material hash is empty: %+v", dep)
		}
	}

	pluginCommit = "cccccccccccccccccccccccccccccccccccccccc"
	second, err := DefaultApplyFingerprintResult(context.Background(), in, ApplyFingerprintOptions{AdapterType: "test"})
	if err != nil {
		t.Fatalf("DefaultApplyFingerprintResult after plugin commit change: %v", err)
	}
	if second.Fingerprint == first.Fingerprint {
		t.Fatalf("fingerprint did not change after resolved Plugin source changed: %s", second.Fingerprint)
	}
}

func TestDefaultApplyFingerprintIgnoresHarnessCompositionWhenDeploymentDoesNotSelectHarness(t *testing.T) {
	in := testApplyInput()
	in.Target = &v1alpha1.Agent{
		TypeMeta: v1alpha1.TypeMeta{Kind: v1alpha1.KindAgent},
		Metadata: v1alpha1.ObjectMeta{
			Namespace: "team-a",
			Name:      "assistant",
		},
		Spec: v1alpha1.AgentSpec{
			CompatibleHarnesses: []v1alpha1.HarnessCompatibility{{Type: "claude-code"}},
			Plugins:             []v1alpha1.ResourceRef{{Name: "deploy-tools"}},
			Skills:              []v1alpha1.ResourceRef{{Name: "weather"}},
			Instructions:        &v1alpha1.ResourceRef{Name: "writer-instructions"},
		},
	}

	in.Getter = func(_ context.Context, ref v1alpha1.ResourceRef) (v1alpha1.Object, error) {
		t.Fatalf("unexpected dependency resolution without deployment harness selection: %+v", ref)
		return nil, nil
	}

	result, err := DefaultApplyFingerprintResult(context.Background(), in, ApplyFingerprintOptions{AdapterType: "test"})
	if err != nil {
		t.Fatalf("DefaultApplyFingerprintResult: %v", err)
	}
	if len(result.Dependencies) != 0 {
		t.Fatalf("dependencies = %+v, want none without deployment harness selection", result.Dependencies)
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

func testPlugin(namespace, name, commit string) *v1alpha1.Plugin {
	return &v1alpha1.Plugin{
		TypeMeta: v1alpha1.TypeMeta{Kind: v1alpha1.KindPlugin},
		Metadata: v1alpha1.ObjectMeta{
			Namespace:  namespace,
			Name:       name,
			UID:        "plugin-uid",
			Generation: 1,
		},
		Spec: v1alpha1.PluginSpec{
			Source: &v1alpha1.PluginSource{
				Type: v1alpha1.PluginSourceTypeGit,
				Git:  &v1alpha1.PluginSourceGit{Repository: &v1alpha1.Repository{URL: "https://github.com/acme/plugin"}},
			},
		},
		Status: v1alpha1.PluginStatus{
			ResolvedSource: &v1alpha1.PluginResolvedSource{Type: v1alpha1.PluginSourceTypeGit, Commit: commit},
		},
	}
}

func testSkill(namespace, name, commit string) *v1alpha1.Skill {
	return &v1alpha1.Skill{
		TypeMeta: v1alpha1.TypeMeta{Kind: v1alpha1.KindSkill},
		Metadata: v1alpha1.ObjectMeta{
			Namespace:  namespace,
			Name:       name,
			UID:        "skill-uid",
			Generation: 1,
		},
		Spec: v1alpha1.SkillSpec{
			Source: &v1alpha1.SkillSource{Repository: &v1alpha1.Repository{URL: "https://github.com/acme/skill"}},
		},
		Status: v1alpha1.SkillStatus{
			ResolvedSource: &v1alpha1.SkillResolvedSource{Commit: commit},
		},
	}
}

func testPrompt(namespace, name, content string) *v1alpha1.Prompt {
	return &v1alpha1.Prompt{
		TypeMeta: v1alpha1.TypeMeta{Kind: v1alpha1.KindPrompt},
		Metadata: v1alpha1.ObjectMeta{
			Namespace:  namespace,
			Name:       name,
			UID:        "prompt-uid",
			Generation: 1,
		},
		Spec: v1alpha1.PromptSpec{Content: content},
	}
}

func testModel(namespace, name, tag, modelID string) *v1alpha1.Model {
	return &v1alpha1.Model{
		TypeMeta: v1alpha1.TypeMeta{Kind: v1alpha1.KindModel},
		Metadata: v1alpha1.ObjectMeta{
			Namespace:  namespace,
			Name:       name,
			Tag:        tag,
			UID:        "model-uid",
			Generation: 1,
		},
		Spec: v1alpha1.ModelSpec{
			Provider: v1alpha1.ModelProviderBedrock,
			Model:    modelID,
		},
	}
}

func testMCPServer(identifier string) *v1alpha1.MCPServer {
	return testMCPServerInNamespace("default", "weather", identifier)
}

func testMCPServerInNamespace(namespace, name, identifier string) *v1alpha1.MCPServer {
	return &v1alpha1.MCPServer{
		TypeMeta: v1alpha1.TypeMeta{Kind: v1alpha1.KindMCPServer},
		Metadata: v1alpha1.ObjectMeta{
			Namespace:  namespace,
			Name:       name,
			UID:        "mcp-uid",
			Generation: 1,
		},
		Spec: v1alpha1.MCPServerSpec{
			Source: &v1alpha1.MCPServerSource{
				Package: &v1alpha1.MCPPackage{
					Origin: v1alpha1.MCPPackageOrigin{
						Type:       v1alpha1.MCPPackageOriginTypeOCI,
						Identifier: identifier,
						OCI:        &v1alpha1.MCPPackageOriginOCI{ServerName: "weather"},
					},
					Transport: v1alpha1.MCPTransport{Type: "stdio"},
				},
			},
		},
	}
}
