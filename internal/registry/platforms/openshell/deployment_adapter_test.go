package openshell

import (
	"context"
	"fmt"
	"testing"

	platformtypes "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/types"
	servicetesting "github.com/agentregistry-dev/agentregistry/internal/registry/service/testing"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

// mockClient implements Client for testing.
type mockClient struct {
	createFn        func(ctx context.Context, opts CreateSandboxOpts) (*SandboxInfo, error)
	getFn           func(ctx context.Context, name string) (*SandboxInfo, error)
	listFn          func(ctx context.Context) ([]SandboxInfo, error)
	deleteFn        func(ctx context.Context, name string) error
	logsFn          func(ctx context.Context, name string) ([]string, error)
	healthFn        func(ctx context.Context) error
	listProvidersFn func(ctx context.Context) ([]ProviderInfo, error)
	ensureProvFn    func(ctx context.Context, name, provType string, creds map[string]string) error
}

func (m *mockClient) CreateSandbox(ctx context.Context, opts CreateSandboxOpts) (*SandboxInfo, error) {
	if m.createFn != nil {
		return m.createFn(ctx, opts)
	}
	return &SandboxInfo{ID: "sb-1", Name: opts.Name, Phase: "SANDBOX_PHASE_PROVISIONING"}, nil
}

func (m *mockClient) GetSandbox(ctx context.Context, name string) (*SandboxInfo, error) {
	if m.getFn != nil {
		return m.getFn(ctx, name)
	}
	return &SandboxInfo{ID: "sb-1", Name: name, Phase: "SANDBOX_PHASE_READY"}, nil
}

func (m *mockClient) ListSandboxes(ctx context.Context) ([]SandboxInfo, error) {
	if m.listFn != nil {
		return m.listFn(ctx)
	}
	return nil, nil
}

func (m *mockClient) DeleteSandbox(ctx context.Context, name string) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, name)
	}
	return nil
}

func (m *mockClient) GetSandboxLogs(ctx context.Context, name string) ([]string, error) {
	if m.logsFn != nil {
		return m.logsFn(ctx, name)
	}
	return nil, nil
}

func (m *mockClient) HealthCheck(ctx context.Context) error {
	if m.healthFn != nil {
		return m.healthFn(ctx)
	}
	return nil
}

func (m *mockClient) ListProviders(ctx context.Context) ([]ProviderInfo, error) {
	if m.listProvidersFn != nil {
		return m.listProvidersFn(ctx)
	}
	return nil, nil
}

func (m *mockClient) EnsureProvider(ctx context.Context, name, provType string, creds map[string]string) error {
	if m.ensureProvFn != nil {
		return m.ensureProvFn(ctx, name, provType, creds)
	}
	return nil
}

func (m *mockClient) Close() error { return nil }

func newTestRegistry() *servicetesting.FakeRegistry {
	return newTestRegistryWithProvider("anthropic")
}

func newTestRegistryWithProvider(modelProvider string) *servicetesting.FakeRegistry {
	registry := servicetesting.NewFakeRegistry()
	registry.GetAgentByNameAndVersionFn = func(_ context.Context, name, version string) (*models.AgentResponse, error) {
		return &models.AgentResponse{
			Agent: models.AgentJSON{
				AgentManifest: models.AgentManifest{
					Name:          name,
					Image:         "test-agent-image:latest",
					ModelProvider: modelProvider,
				},
				Version: version,
			},
		}, nil
	}
	registry.ResolveAgentManifestSkillsFn = func(_ context.Context, _ *models.AgentManifest) ([]platformtypes.AgentSkillRef, error) {
		return nil, nil
	}
	registry.ResolveAgentManifestPromptsFn = func(_ context.Context, _ *models.AgentManifest) ([]platformtypes.ResolvedPrompt, error) {
		return nil, nil
	}
	return registry
}

func TestDeploy_Agent_CallsCreateSandbox(t *testing.T) {
	var createdOpts CreateSandboxOpts
	client := &mockClient{
		createFn: func(_ context.Context, opts CreateSandboxOpts) (*SandboxInfo, error) {
			createdOpts = opts
			return &SandboxInfo{ID: "sb-1", Name: opts.Name, Phase: "SANDBOX_PHASE_PROVISIONING"}, nil
		},
	}

	registry := newTestRegistry()
	adapter := NewOpenShellDeploymentAdapter(registry, client)

	deployment := &models.Deployment{
		ID:           "dep-123",
		ServerName:   "my-agent",
		Version:      "1.0.0",
		ResourceType: "agent",
		ProviderID:   "openshell-default",
		Env:          map[string]string{"MY_KEY": "my-val"},
	}

	result, err := adapter.Deploy(context.Background(), deployment)
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}
	if result.Status != models.DeploymentStatusDeployed {
		t.Errorf("status = %q, want %q", result.Status, models.DeploymentStatusDeployed)
	}
	if result.ProviderMetadata == nil || result.ProviderMetadata["openshellForwardCLI"] == nil {
		t.Errorf("expected openshell ProviderMetadata with forward CLI, got %#v", result.ProviderMetadata)
	}
	if createdOpts.Image != "test-agent-image:latest" {
		t.Errorf("image = %q, want %q", createdOpts.Image, "test-agent-image:latest")
	}
	if createdOpts.Name == "" {
		t.Error("sandbox name should not be empty")
	}
	if createdOpts.Policy == nil || createdOpts.Policy.GetProcess().GetRunAsUser() != "sandbox" {
		t.Errorf("expected default sandbox policy with run_as_user sandbox, got policy=%v", createdOpts.Policy)
	}
	np := createdOpts.Policy.GetNetworkPolicies()
	rule, ok := np["anthropic_api"]
	if !ok || rule == nil || len(rule.GetEndpoints()) == 0 || rule.GetEndpoints()[0].GetHost() != "api.anthropic.com" {
		t.Errorf("anthropic agent should get api.anthropic.com egress, got network_policies=%v", np)
	}
	if len(createdOpts.Command) < 2 || createdOpts.Command[0] != "kagent-adk" || createdOpts.Command[1] != "run" {
		t.Errorf("command = %v, want kagent-adk run ...", createdOpts.Command)
	}
	if createdOpts.Env["KAGENT_URL"] != "placeholder" || createdOpts.Env["KAGENT_NAMESPACE"] != "placeholder" {
		t.Errorf("KAGENT env = %#v, want placeholder URL/namespace", createdOpts.Env)
	}
	if createdOpts.Env["KAGENT_NAME"] != "my-agent" {
		t.Errorf("KAGENT_NAME = %q, want my-agent", createdOpts.Env["KAGENT_NAME"])
	}
	wantWorkload := "kagent-adk run --host 0.0.0.0 --port 9999 my-agent --local"
	if got := createdOpts.Env["OPENSHELL_SANDBOX_COMMAND"]; got != wantWorkload {
		t.Errorf("OPENSHELL_SANDBOX_COMMAND = %q, want %q", got, wantWorkload)
	}
}

func TestDeploy_MCP_CallsCreateSandbox(t *testing.T) {
	var createdOpts CreateSandboxOpts
	client := &mockClient{
		createFn: func(_ context.Context, opts CreateSandboxOpts) (*SandboxInfo, error) {
			createdOpts = opts
			return &SandboxInfo{ID: "sb-2", Name: opts.Name, Phase: "SANDBOX_PHASE_PROVISIONING"}, nil
		},
	}

	registry := newTestRegistry()
	registry.GetServerByNameAndVersionFn = func(_ context.Context, name, version string) (*apiv0.ServerResponse, error) {
		return &apiv0.ServerResponse{
			Server: apiv0.ServerJSON{
				Name:    name,
				Version: version,
				Packages: []model.Package{
					{
						RegistryType: "oci",
						Identifier:   "test-mcp-image:latest",
					},
				},
			},
		}, nil
	}

	adapter := NewOpenShellDeploymentAdapter(registry, client)

	deployment := &models.Deployment{
		ID:           "dep-456",
		ServerName:   "my-mcp",
		Version:      "2.0.0",
		ResourceType: "mcp",
		ProviderID:   "openshell-default",
	}

	result, err := adapter.Deploy(context.Background(), deployment)
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}
	if result.Status != models.DeploymentStatusDeployed {
		t.Errorf("status = %q, want %q", result.Status, models.DeploymentStatusDeployed)
	}
	if createdOpts.Name == "" {
		t.Error("sandbox name should not be empty")
	}
}

func TestUndeploy_CallsDeleteSandbox(t *testing.T) {
	var deletedName string
	client := &mockClient{
		deleteFn: func(_ context.Context, name string) error {
			deletedName = name
			return nil
		},
	}

	adapter := NewOpenShellDeploymentAdapter(newTestRegistry(), client)

	deployment := &models.Deployment{
		ID:           "dep-789",
		ServerName:   "my-agent",
		Version:      "1.0.0",
		ResourceType: "agent",
		ProviderID:   "openshell-default",
	}

	if err := adapter.Undeploy(context.Background(), deployment); err != nil {
		t.Fatalf("Undeploy() error = %v", err)
	}
	if deletedName == "" {
		t.Error("expected DeleteSandbox to be called")
	}
}

func TestGetLogs_ReturnsLogLines(t *testing.T) {
	client := &mockClient{
		logsFn: func(_ context.Context, _ string) ([]string, error) {
			return []string{"line1", "line2", "line3"}, nil
		},
	}

	adapter := NewOpenShellDeploymentAdapter(newTestRegistry(), client)

	deployment := &models.Deployment{
		ID:           "dep-logs",
		ServerName:   "my-agent",
		Version:      "1.0.0",
		ResourceType: "agent",
		ProviderID:   "openshell-default",
	}

	logs, err := adapter.GetLogs(context.Background(), deployment)
	if err != nil {
		t.Fatalf("GetLogs() error = %v", err)
	}
	if len(logs) != 3 {
		t.Errorf("log lines = %d, want 3", len(logs))
	}
}

func TestDeploy_ValidationError(t *testing.T) {
	adapter := NewOpenShellDeploymentAdapter(newTestRegistry(), &mockClient{})

	tests := []struct {
		name       string
		deployment *models.Deployment
	}{
		{"nil deployment", nil},
		{"missing provider id", &models.Deployment{
			ServerName:   "test",
			Version:      "1.0.0",
			ResourceType: "agent",
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := adapter.Deploy(context.Background(), tt.deployment)
			if err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestDeploy_WaitsForReadyPhase(t *testing.T) {
	callCount := 0
	client := &mockClient{
		getFn: func(_ context.Context, name string) (*SandboxInfo, error) {
			callCount++
			if callCount < 3 {
				return &SandboxInfo{ID: "sb-1", Name: name, Phase: "SANDBOX_PHASE_PROVISIONING"}, nil
			}
			return &SandboxInfo{ID: "sb-1", Name: name, Phase: "SANDBOX_PHASE_READY"}, nil
		},
	}

	adapter := NewOpenShellDeploymentAdapter(newTestRegistry(), client)

	deployment := &models.Deployment{
		ID:           "dep-poll",
		ServerName:   "poll-agent",
		Version:      "1.0.0",
		ResourceType: "agent",
		ProviderID:   "openshell-default",
	}

	result, err := adapter.Deploy(context.Background(), deployment)
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}
	if result.Status != models.DeploymentStatusDeployed {
		t.Errorf("status = %q, want %q", result.Status, models.DeploymentStatusDeployed)
	}
	if callCount < 3 {
		t.Errorf("expected at least 3 GetSandbox polls, got %d", callCount)
	}
}

func TestDeploy_ErrorPhase(t *testing.T) {
	client := &mockClient{
		getFn: func(_ context.Context, name string) (*SandboxInfo, error) {
			return &SandboxInfo{ID: "sb-1", Name: name, Phase: "SANDBOX_PHASE_ERROR"}, nil
		},
	}

	adapter := NewOpenShellDeploymentAdapter(newTestRegistry(), client)

	deployment := &models.Deployment{
		ID:           "dep-err",
		ServerName:   "err-agent",
		Version:      "1.0.0",
		ResourceType: "agent",
		ProviderID:   "openshell-default",
	}

	_, err := adapter.Deploy(context.Background(), deployment)
	if err == nil {
		t.Fatal("expected error for sandbox in Error phase")
	}
}

func TestPlatform(t *testing.T) {
	adapter := NewOpenShellDeploymentAdapter(newTestRegistry(), &mockClient{})
	if p := adapter.Platform(); p != "openshell" {
		t.Errorf("Platform() = %q, want %q", p, "openshell")
	}
}

func TestSupportedResourceTypes(t *testing.T) {
	adapter := NewOpenShellDeploymentAdapter(newTestRegistry(), &mockClient{})
	types := adapter.SupportedResourceTypes()
	if len(types) != 2 {
		t.Errorf("SupportedResourceTypes() len = %d, want 2", len(types))
	}
}

func TestDiscover_ReturnsEmpty(t *testing.T) {
	adapter := NewOpenShellDeploymentAdapter(newTestRegistry(), &mockClient{})
	result, err := adapter.Discover(context.Background(), "any")
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(result) != 0 {
		t.Errorf("Discover() len = %d, want 0", len(result))
	}
}

func TestCancel_DeletesSandbox(t *testing.T) {
	var deletedName string
	client := &mockClient{
		deleteFn: func(_ context.Context, name string) error {
			deletedName = name
			return nil
		},
	}

	adapter := NewOpenShellDeploymentAdapter(newTestRegistry(), client)
	deployment := &models.Deployment{
		ID:           "dep-cancel",
		ServerName:   "cancel-agent",
		Version:      "1.0.0",
		ResourceType: "agent",
		ProviderID:   "openshell-default",
	}

	if err := adapter.Cancel(context.Background(), deployment); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if deletedName == "" {
		t.Error("expected DeleteSandbox to be called")
	}
}

func TestMergeEnv(t *testing.T) {
	base := map[string]string{"A": "1", "B": "2"}
	override := map[string]string{"B": "3", "C": "4"}
	result := mergeEnv(base, override)
	if result["A"] != "1" {
		t.Errorf("A = %q, want 1", result["A"])
	}
	if result["B"] != "3" {
		t.Errorf("B = %q, want 3 (override)", result["B"])
	}
	if result["C"] != "4" {
		t.Errorf("C = %q, want 4", result["C"])
	}
}

func TestSandboxNameForDeployment(t *testing.T) {
	deployment := &models.Deployment{
		ID:         "dep-123",
		ServerName: "my-agent",
	}
	name := sandboxNameForDeployment(deployment)
	if name == "" {
		t.Error("expected non-empty sandbox name")
	}
}

func TestSandboxNameForDeployment_Nil(t *testing.T) {
	name := sandboxNameForDeployment(nil)
	if name != "" {
		t.Errorf("expected empty name for nil deployment, got %q", name)
	}
}

func TestDeploy_CreateSandboxError(t *testing.T) {
	client := &mockClient{
		createFn: func(_ context.Context, _ CreateSandboxOpts) (*SandboxInfo, error) {
			return nil, fmt.Errorf("connection refused")
		},
	}

	adapter := NewOpenShellDeploymentAdapter(newTestRegistry(), client)

	deployment := &models.Deployment{
		ID:           "dep-fail",
		ServerName:   "fail-agent",
		Version:      "1.0.0",
		ResourceType: "agent",
		ProviderID:   "openshell-default",
	}

	_, err := adapter.Deploy(context.Background(), deployment)
	if err == nil {
		t.Fatal("expected error from CreateSandbox failure")
	}
}

func TestDeploy_Agent_EnsuresProvider(t *testing.T) {
	var ensuredName, ensuredType string
	var ensuredCreds map[string]string
	var createdProviders []string

	client := &mockClient{
		ensureProvFn: func(_ context.Context, name, provType string, creds map[string]string) error {
			ensuredName = name
			ensuredType = provType
			ensuredCreds = creds
			return nil
		},
		createFn: func(_ context.Context, opts CreateSandboxOpts) (*SandboxInfo, error) {
			createdProviders = opts.Providers
			return &SandboxInfo{ID: "sb-1", Name: opts.Name, Phase: "SANDBOX_PHASE_PROVISIONING"}, nil
		},
	}

	adapter := NewOpenShellDeploymentAdapter(newTestRegistry(), client)

	deployment := &models.Deployment{
		ID:           "dep-prov",
		ServerName:   "my-agent",
		Version:      "1.0.0",
		ResourceType: "agent",
		ProviderID:   "openshell-default",
		Env:          map[string]string{"ANTHROPIC_API_KEY": "sk-test-123"},
	}

	result, err := adapter.Deploy(context.Background(), deployment)
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}
	if result.Status != models.DeploymentStatusDeployed {
		t.Errorf("status = %q, want %q", result.Status, models.DeploymentStatusDeployed)
	}
	if ensuredName != "anthropic" {
		t.Errorf("ensured provider name = %q, want %q", ensuredName, "anthropic")
	}
	if ensuredType != "claude" {
		t.Errorf("ensured provider type = %q, want %q (OpenShell type for anthropic)", ensuredType, "claude")
	}
	if ensuredCreds["ANTHROPIC_API_KEY"] != "sk-test-123" {
		t.Errorf("ensured creds = %v, want ANTHROPIC_API_KEY=sk-test-123", ensuredCreds)
	}
	if len(createdProviders) != 1 || createdProviders[0] != "anthropic" {
		t.Errorf("sandbox providers = %v, want [anthropic]", createdProviders)
	}
}

func TestDeploy_Agent_NoProviderWhenModelProviderEmpty(t *testing.T) {
	var ensureCalled bool
	var createdOpts CreateSandboxOpts
	client := &mockClient{
		createFn: func(_ context.Context, opts CreateSandboxOpts) (*SandboxInfo, error) {
			createdOpts = opts
			return &SandboxInfo{ID: "sb-1", Name: opts.Name, Phase: "SANDBOX_PHASE_PROVISIONING"}, nil
		},
		ensureProvFn: func(_ context.Context, _, _ string, _ map[string]string) error {
			ensureCalled = true
			return nil
		},
	}

	adapter := NewOpenShellDeploymentAdapter(newTestRegistryWithProvider(""), client)

	deployment := &models.Deployment{
		ID:           "dep-noprov",
		ServerName:   "my-agent",
		Version:      "1.0.0",
		ResourceType: "agent",
		ProviderID:   "openshell-default",
	}

	_, err := adapter.Deploy(context.Background(), deployment)
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}
	if ensureCalled {
		t.Error("EnsureProvider should not be called when model provider is empty")
	}
	if _, ok := createdOpts.Policy.GetNetworkPolicies()["gemini_api"]; !ok {
		t.Errorf("empty MODEL_PROVIDER should default to Gemini network policy, got %#v", createdOpts.Policy.GetNetworkPolicies())
	}
}

func TestDeploy_Agent_EnsureProviderError(t *testing.T) {
	client := &mockClient{
		ensureProvFn: func(_ context.Context, _, _ string, _ map[string]string) error {
			return fmt.Errorf("gateway unreachable")
		},
	}

	adapter := NewOpenShellDeploymentAdapter(newTestRegistry(), client)

	deployment := &models.Deployment{
		ID:           "dep-enserr",
		ServerName:   "my-agent",
		Version:      "1.0.0",
		ResourceType: "agent",
		ProviderID:   "openshell-default",
		Env:          map[string]string{"ANTHROPIC_API_KEY": "sk-test"},
	}

	_, err := adapter.Deploy(context.Background(), deployment)
	if err == nil {
		t.Fatal("expected error from EnsureProvider failure")
	}
}

func TestExtractProviderCredentials(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		env      map[string]string
		wantKeys []string
	}{
		{
			name:     "anthropic with key",
			provider: "anthropic",
			env:      map[string]string{"ANTHROPIC_API_KEY": "sk-123", "OTHER": "val"},
			wantKeys: []string{"ANTHROPIC_API_KEY"},
		},
		{
			name:     "openai with key",
			provider: "openai",
			env:      map[string]string{"OPENAI_API_KEY": "sk-456"},
			wantKeys: []string{"OPENAI_API_KEY"},
		},
		{
			name:     "google with gemini key",
			provider: "google",
			env:      map[string]string{"GEMINI_API_KEY": "gem-789"},
			wantKeys: []string{"GEMINI_API_KEY"},
		},
		{
			name:     "unknown provider falls back to generic",
			provider: "custom-llm",
			env:      map[string]string{"CUSTOM_KEY": "val"},
			wantKeys: nil,
		},
		{
			name:     "known provider but no matching env var",
			provider: "anthropic",
			env:      map[string]string{"SOME_OTHER_KEY": "val"},
			wantKeys: nil,
		},
		{
			name:     "empty env var value ignored",
			provider: "anthropic",
			env:      map[string]string{"ANTHROPIC_API_KEY": ""},
			wantKeys: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := resolveProviderSpec(tt.provider)
			creds := extractProviderCredentials(spec, tt.env)
			if len(tt.wantKeys) == 0 {
				if len(creds) != 0 {
					t.Errorf("expected empty creds, got %v", creds)
				}
				return
			}
			for _, key := range tt.wantKeys {
				if _, ok := creds[key]; !ok {
					t.Errorf("expected key %q in creds %v", key, creds)
				}
			}
		})
	}
}

func TestResolveProviderSpec(t *testing.T) {
	tests := []struct {
		provider string
		wantType string
	}{
		{"anthropic", "claude"},
		{"openai", "openai"},
		{"nvidia", "nvidia"},
		{"google", "generic"},
		{"gemini", "generic"},
		{"mistral", "generic"},
		{"cohere", "generic"},
		{"unknown-provider", "generic"},
	}

	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			spec := resolveProviderSpec(tt.provider)
			if spec.Type != tt.wantType {
				t.Errorf("resolveProviderSpec(%q).Type = %q, want %q", tt.provider, spec.Type, tt.wantType)
			}
		})
	}
}
