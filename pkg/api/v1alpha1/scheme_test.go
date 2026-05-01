package v1alpha1

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

func TestScheme_RegisterAllBuiltins(t *testing.T) {
	got := Default.Kinds()
	want := []string{"agent", "deployment", "mcpserver", "prompt", "provider", "remotemcpserver", "skill"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("built-in kinds = %v, want %v", got, want)
	}
}

func TestScheme_Register_Duplicate(t *testing.T) {
	s := NewScheme()
	if err := s.Register("Foo", struct{}{}, func() any { return &struct{}{} }); err != nil {
		t.Fatalf("first register failed: %v", err)
	}
	if err := s.Register("foo", struct{}{}, func() any { return &struct{}{} }); err == nil {
		t.Fatal("expected duplicate registration (case-insensitive) to fail")
	}
}

func TestScheme_Decode_Agent(t *testing.T) {
	doc := []byte(`
apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  name: summarizer
  version: "1.0.0"
  labels:
    owner: core
spec:
  title: Summarizer
  source:
    image: ghcr.io/example/summarizer:1.0.0
  language: go
  modelProvider: openai
  modelName: gpt-4o
  mcpServers:
    - kind: MCPServer
      name: github-tools
      version: "0.2"
  skills:
    - kind: Skill
      name: code-review
`)
	obj, err := Default.Decode(doc)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	agent, ok := obj.(*Agent)
	if !ok {
		t.Fatalf("want *Agent, got %T", obj)
	}
	if agent.APIVersion != GroupVersion || agent.Kind != KindAgent {
		t.Fatalf("envelope mismatch: %+v", agent.TypeMeta)
	}
	if agent.Metadata.Name != "summarizer" || agent.Metadata.Version != "1.0.0" {
		t.Fatalf("metadata mismatch: %+v", agent.Metadata)
	}
	if agent.Metadata.Labels["owner"] != "core" {
		t.Fatalf("labels mismatch: %+v", agent.Metadata.Labels)
	}
	if agent.Spec.Source.Image != "ghcr.io/example/summarizer:1.0.0" {
		t.Fatalf("spec.source.image mismatch: %q", agent.Spec.Source.Image)
	}
	if len(agent.Spec.MCPServers) != 1 ||
		agent.Spec.MCPServers[0].Kind != KindMCPServer ||
		agent.Spec.MCPServers[0].Name != "github-tools" ||
		agent.Spec.MCPServers[0].Version != "0.2" {
		t.Fatalf("mcpServers ref mismatch: %+v", agent.Spec.MCPServers)
	}
	if len(agent.Spec.Skills) != 1 || agent.Spec.Skills[0].Name != "code-review" {
		t.Fatalf("skills ref mismatch: %+v", agent.Spec.Skills)
	}
}

func TestScheme_Decode_MCPServer(t *testing.T) {
	doc := []byte(`
apiVersion: ar.dev/v1alpha1
kind: MCPServer
metadata:
  name: github-tools
  version: "0.2"
spec:
  title: GitHub Tools
  packages:
    - registryType: oci
      identifier: ghcr.io/example/mcp-github
      version: "0.2"
      transport:
        type: stdio
`)
	obj, err := Default.Decode(doc)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	m, ok := obj.(*MCPServer)
	if !ok {
		t.Fatalf("want *MCPServer, got %T", obj)
	}
	if len(m.Spec.Packages) != 1 || m.Spec.Packages[0].Identifier != "ghcr.io/example/mcp-github" {
		t.Fatalf("packages mismatch: %+v", m.Spec.Packages)
	}
	if m.Spec.Packages[0].Transport.Type != "stdio" {
		t.Fatalf("transport mismatch: %+v", m.Spec.Packages[0].Transport)
	}
}

func TestScheme_Decode_Deployment(t *testing.T) {
	doc := []byte(`
apiVersion: ar.dev/v1alpha1
kind: Deployment
metadata:
  name: summarizer-prod
spec:
  targetRef:
    kind: Agent
    name: summarizer
    version: "1.0.0"
  providerRef:
    kind: Provider
    name: local
  desiredState: deployed
  env:
    OPENAI_API_KEY: "{{ .Secrets.openai }}"
`)
	obj, err := Default.Decode(doc)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	d, ok := obj.(*Deployment)
	if !ok {
		t.Fatalf("want *Deployment, got %T", obj)
	}
	if d.Spec.TargetRef.Kind != KindAgent || d.Spec.TargetRef.Name != "summarizer" {
		t.Fatalf("targetRef mismatch: %+v", d.Spec.TargetRef)
	}
	if d.Spec.DesiredState != DesiredStateDeployed {
		t.Fatalf("desiredState mismatch: %q", d.Spec.DesiredState)
	}
}

func TestScheme_Decode_RejectsWrongAPIVersion(t *testing.T) {
	doc := []byte(`
apiVersion: example.com/v1
kind: Agent
metadata: { name: x }
`)
	if _, err := Default.Decode(doc); err == nil ||
		!strings.Contains(err.Error(), "unsupported apiVersion") {
		t.Fatalf("expected unsupported apiVersion error; got %v", err)
	}
}

func TestScheme_Decode_RejectsUnknownKind(t *testing.T) {
	doc := []byte(`
apiVersion: ar.dev/v1alpha1
kind: Gadget
metadata: { name: x }
`)
	if _, err := Default.Decode(doc); err == nil ||
		!strings.Contains(err.Error(), `unknown kind "Gadget"`) {
		t.Fatalf("expected unknown kind error; got %v", err)
	}
}

func TestScheme_Decode_RejectsMissingKind(t *testing.T) {
	doc := []byte(`apiVersion: ar.dev/v1alpha1`)
	if _, err := Default.Decode(doc); err == nil ||
		!strings.Contains(err.Error(), "missing kind") {
		t.Fatalf("expected missing kind error; got %v", err)
	}
}

func TestScheme_DecodeMulti_Stream(t *testing.T) {
	doc := []byte(`apiVersion: ar.dev/v1alpha1
kind: Skill
metadata:
  name: a
spec:
  title: A
---
apiVersion: ar.dev/v1alpha1
kind: Prompt
metadata:
  name: b
spec:
  content: hi
---
apiVersion: ar.dev/v1alpha1
kind: Provider
metadata:
  name: local
spec:
  platform: local
`)
	objs, err := Default.DecodeMulti(doc)
	if err != nil {
		t.Fatalf("DecodeMulti: %v", err)
	}
	if len(objs) != 3 {
		t.Fatalf("want 3 docs, got %d", len(objs))
	}
	if _, ok := objs[0].(*Skill); !ok {
		t.Fatalf("doc 0: want *Skill, got %T", objs[0])
	}
	if _, ok := objs[1].(*Prompt); !ok {
		t.Fatalf("doc 1: want *Prompt, got %T", objs[1])
	}
	if _, ok := objs[2].(*Provider); !ok {
		t.Fatalf("doc 2: want *Provider, got %T", objs[2])
	}
}

func TestScheme_DecodeMulti_SkipsEmptyDocs(t *testing.T) {
	doc := []byte(`---
apiVersion: ar.dev/v1alpha1
kind: Prompt
metadata: { name: solo }
spec: { content: x }
---
---
`)
	objs, err := Default.DecodeMulti(doc)
	if err != nil {
		t.Fatalf("DecodeMulti: %v", err)
	}
	if len(objs) != 1 {
		t.Fatalf("want 1 doc (rest empty), got %d", len(objs))
	}
}

func TestScheme_DecodeMulti_EmptyInput(t *testing.T) {
	objs, err := Default.DecodeMulti([]byte(" \n\t"))
	if err != nil {
		t.Fatalf("DecodeMulti: %v", err)
	}
	if len(objs) != 0 {
		t.Fatalf("want 0 docs for empty input, got %d", len(objs))
	}
}

func TestScheme_DecodeInto_OK(t *testing.T) {
	doc := []byte(`
apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  name: summarizer
  version: "1.0.0"
spec:
  title: Summarizer
`)
	var got Agent
	if err := Default.DecodeInto(doc, &got); err != nil {
		t.Fatalf("DecodeInto: %v", err)
	}
	if got.Kind != KindAgent || got.Metadata.Name != "summarizer" {
		t.Fatalf("decoded envelope mismatch: %+v", got)
	}
}

func TestScheme_DecodeInto_RejectsKindMismatch(t *testing.T) {
	doc := []byte(`
apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  name: summarizer
spec:
  title: Summarizer
`)
	var got MCPServer
	err := Default.DecodeInto(doc, &got)
	if err == nil || !strings.Contains(err.Error(), "does not match decoded type") {
		t.Fatalf("expected type mismatch error, got %v", err)
	}
}

func TestEncode_RoundTrip_YAML(t *testing.T) {
	// Empty Namespace survives a round trip — MarshalJSON strips "default"
	// but UnmarshalJSON intentionally does NOT re-stamp it, so callers
	// like the importer can layer their own default on top.
	original := &Agent{
		TypeMeta: TypeMeta{APIVersion: GroupVersion, Kind: KindAgent},
		Metadata: ObjectMeta{Name: "rt", Version: "v1", Labels: map[string]string{"k": "v"}},
		Spec: AgentSpec{
			Title:      "Round Trip",
			Source:     &AgentSource{Image: "img:tag"},
			MCPServers: []ResourceRef{{Kind: KindMCPServer, Name: "mcp1", Version: "1"}},
		},
	}

	out, err := Encode(original)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decoded, err := Default.Decode(out)
	if err != nil {
		t.Fatalf("Decode after Encode: %v\nyaml=%s", err, out)
	}
	got, ok := decoded.(*Agent)
	if !ok {
		t.Fatalf("want *Agent, got %T", decoded)
	}
	if !reflect.DeepEqual(original.Spec, got.Spec) {
		t.Fatalf("spec round-trip mismatch\nwant: %+v\ngot:  %+v", original.Spec, got.Spec)
	}
	if !reflect.DeepEqual(original.Metadata, got.Metadata) {
		t.Fatalf("metadata round-trip mismatch\nwant: %+v\ngot:  %+v", original.Metadata, got.Metadata)
	}
}

func TestEncode_RoundTrip_JSON(t *testing.T) {
	original := &Deployment{
		TypeMeta: TypeMeta{APIVersion: GroupVersion, Kind: KindDeployment},
		Metadata: ObjectMeta{Name: "prod", Version: "1"},
		Spec: DeploymentSpec{
			TargetRef:    ResourceRef{Kind: KindAgent, Name: "x", Version: "1"},
			ProviderRef:  ResourceRef{Kind: KindProvider, Name: "local"},
			DesiredState: DesiredStateDeployed,
			Env:          map[string]string{"FOO": "bar"},
		},
	}
	out, err := EncodeJSON(original)
	if err != nil {
		t.Fatalf("EncodeJSON: %v", err)
	}
	var got Deployment
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(original.Spec, &got.Spec) && !reflect.DeepEqual(*original, got) {
		t.Fatalf("round-trip mismatch\nwant: %+v\ngot:  %+v", original, got)
	}
}

// Sanity: ensure we can point sigs.k8s.io/yaml at typed envelopes too.
func TestYAMLDirect_EncodeDecodeProvider(t *testing.T) {
	p := &Provider{
		TypeMeta: TypeMeta{APIVersion: GroupVersion, Kind: KindProvider},
		Metadata: ObjectMeta{Name: "k8s"},
		Spec:     ProviderSpec{Platform: PlatformKubernetes, Config: map[string]any{"namespace": "agentregistry"}},
	}
	y, err := yaml.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got Provider
	if err := yaml.Unmarshal(y, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Spec.Platform != PlatformKubernetes ||
		got.Spec.Config["namespace"] != "agentregistry" {
		t.Fatalf("yaml round-trip mismatch: %+v", got)
	}
}
