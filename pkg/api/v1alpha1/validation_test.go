package v1alpha1

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// Helper: extract field paths from a Validate() result so tests can
// assert on "which fields failed" rather than on full error messages.
func failedFields(t *testing.T, err error) []string {
	t.Helper()
	if err == nil {
		return nil
	}
	var fe FieldErrors
	require.ErrorAs(t, err, &fe, "expected FieldErrors, got %T: %v", err, err)
	paths := make([]string, len(fe))
	for i, e := range fe {
		paths[i] = e.Path
	}
	return paths
}

// -----------------------------------------------------------------------------
// ObjectMeta
// -----------------------------------------------------------------------------

func TestValidateObjectMeta_OK(t *testing.T) {
	m := ObjectMeta{Namespace: "default", Name: "alice", Version: "v1.0.0"}
	require.Empty(t, ValidateObjectMeta(m))
}

func TestValidateObjectMeta_RejectsMissing(t *testing.T) {
	errs := ValidateObjectMeta(ObjectMeta{})
	paths := make([]string, len(errs))
	for i, e := range errs {
		paths[i] = e.Path
	}
	require.Contains(t, paths, "metadata.namespace")
	require.Contains(t, paths, "metadata.name")
	require.Contains(t, paths, "metadata.version")
}

func TestValidateObjectMeta_RejectsBadNamespace(t *testing.T) {
	for _, bad := range []string{"UPPER", "has spaces", "has_underscore", "ai.exa/exa", "-leading", "trailing-"} {
		errs := ValidateObjectMeta(ObjectMeta{Namespace: bad, Name: "x", Version: "v1"})
		require.NotEmpty(t, errs, "namespace %q should be invalid", bad)
	}
}

func TestValidateObjectMeta_AcceptsDNSStyleName(t *testing.T) {
	// Names can carry slashes (dns-like). Namespaces cannot.
	errs := ValidateObjectMeta(ObjectMeta{Namespace: "default", Name: "ai.exa/exa", Version: "v1.0.0"})
	require.Empty(t, errs)
}

func TestValidateObjectMeta_RejectsVersionLatest(t *testing.T) {
	errs := ValidateObjectMeta(ObjectMeta{Namespace: "default", Name: "x", Version: "latest"})
	require.NotEmpty(t, errs)
	require.ErrorIs(t, errs[0].Cause, ErrInvalidVersion)
}

func TestValidateObjectMeta_RejectsVersionRange(t *testing.T) {
	for _, bad := range []string{"^1.0.0", "~1.2", ">=1.0.0", "1.x", "1.0.0 || 2.0.0", "1.0.0, 2.0.0", "*"} {
		errs := ValidateObjectMeta(ObjectMeta{Namespace: "default", Name: "x", Version: bad})
		require.NotEmpty(t, errs, "version %q should be rejected", bad)
	}
}

func TestValidateObjectMeta_AcceptsPinnedVersions(t *testing.T) {
	for _, ok := range []string{"1.0.0", "v1.0.0", "v1.2.3-beta.1", "2024.04.17", "abc123"} {
		errs := ValidateObjectMeta(ObjectMeta{Namespace: "default", Name: "x", Version: ok})
		require.Empty(t, errs, "version %q should be accepted", ok)
	}
}

func TestValidateObjectMeta_RejectsBadLabelKey(t *testing.T) {
	errs := ValidateObjectMeta(ObjectMeta{
		Namespace: "default", Name: "x", Version: "v1",
		Labels: map[string]string{"has spaces": "v"},
	})
	require.NotEmpty(t, errs)
}

// -----------------------------------------------------------------------------
// AgentSpec
// -----------------------------------------------------------------------------

func TestAgentValidate_OK(t *testing.T) {
	a := &Agent{
		TypeMeta: TypeMeta{APIVersion: GroupVersion, Kind: KindAgent},
		Metadata: ObjectMeta{Namespace: "default", Name: "alice", Version: "v1"},
		Spec: AgentSpec{
			Title: "Alice",
			MCPServers: []ResourceRef{
				{Kind: KindMCPServer, Name: "tools", Version: "v1"},
			},
		},
	}
	require.NoError(t, a.Validate())
}

func TestAgentValidate_RejectsWrongRefKind(t *testing.T) {
	a := &Agent{
		Metadata: ObjectMeta{Namespace: "default", Name: "a", Version: "v1"},
		Spec: AgentSpec{
			MCPServers: []ResourceRef{{Kind: KindSkill, Name: "wrong", Version: "v1"}},
		},
	}
	paths := failedFields(t, a.Validate())
	require.Contains(t, paths, "spec.mcpServers[0].kind")
}

func TestAgentValidate_RejectsBadWebsiteURL(t *testing.T) {
	for _, bad := range []string{"http://example.com", "not-a-url", "ftp://example.com"} {
		a := &Agent{
			Metadata: ObjectMeta{Namespace: "default", Name: "a", Version: "v1"},
			Spec:     AgentSpec{WebsiteURL: bad},
		}
		paths := failedFields(t, a.Validate())
		require.Contains(t, paths, "spec.websiteUrl", "url %q should fail", bad)
	}
}

func TestAgentValidate_AcceptsBlankOptionalFields(t *testing.T) {
	a := &Agent{
		Metadata: ObjectMeta{Namespace: "default", Name: "minimal", Version: "v1"},
		Spec:     AgentSpec{}, // everything blank
	}
	require.NoError(t, a.Validate())
}

func TestAgentValidate_AccumulatesErrors(t *testing.T) {
	a := &Agent{
		Metadata: ObjectMeta{Namespace: "default", Name: "a", Version: "v1"},
		Spec: AgentSpec{
			Title:      "   ", // whitespace only
			WebsiteURL: "ftp://x",
			Packages: []AgentPackage{
				{}, // missing registryType, identifier, version
			},
		},
	}
	paths := failedFields(t, a.Validate())
	require.Contains(t, paths, "spec.title")
	require.Contains(t, paths, "spec.websiteUrl")
	require.Contains(t, paths, "spec.packages[0].registryType")
}

func TestAgentResolveRefs_OK(t *testing.T) {
	resolver := func(ctx context.Context, ref ResourceRef) error { return nil }
	a := &Agent{
		Metadata: ObjectMeta{Namespace: "default", Name: "a", Version: "v1"},
		Spec: AgentSpec{
			MCPServers: []ResourceRef{{Kind: KindMCPServer, Name: "tools", Version: "v1"}},
			Skills:     []ResourceRef{{Kind: KindSkill, Name: "code-review", Version: "v1"}},
		},
	}
	require.NoError(t, a.ResolveRefs(context.Background(), resolver))
}

func TestAgentResolveRefs_ReportsDangling(t *testing.T) {
	resolver := func(ctx context.Context, ref ResourceRef) error {
		if ref.Name == "missing" {
			return ErrDanglingRef
		}
		return nil
	}
	a := &Agent{
		Metadata: ObjectMeta{Namespace: "default", Name: "a", Version: "v1"},
		Spec: AgentSpec{
			MCPServers: []ResourceRef{
				{Kind: KindMCPServer, Name: "tools", Version: "v1"},
				{Kind: KindMCPServer, Name: "missing", Version: "v1"},
			},
		},
	}
	err := a.ResolveRefs(context.Background(), resolver)
	require.Error(t, err)
	require.Contains(t, err.Error(), "spec.mcpServers[1]")
}

func TestAgentResolveRefs_InheritsNamespace(t *testing.T) {
	var seen []ResourceRef
	resolver := func(ctx context.Context, ref ResourceRef) error {
		seen = append(seen, ref)
		return nil
	}
	a := &Agent{
		Metadata: ObjectMeta{Namespace: "team-a", Name: "a", Version: "v1"},
		Spec: AgentSpec{
			MCPServers: []ResourceRef{
				// blank namespace should inherit Agent's "team-a"
				{Kind: KindMCPServer, Name: "local-tools", Version: "v1"},
				// explicit namespace sticks
				{Kind: KindMCPServer, Namespace: "shared", Name: "common", Version: "v1"},
			},
		},
	}
	require.NoError(t, a.ResolveRefs(context.Background(), resolver))
	require.Len(t, seen, 2)
	require.Equal(t, "team-a", seen[0].Namespace)
	require.Equal(t, "shared", seen[1].Namespace)
}

func TestAgentResolveRefs_NilResolverIsNoOp(t *testing.T) {
	a := &Agent{Metadata: ObjectMeta{Namespace: "default", Name: "a", Version: "v1"}}
	require.NoError(t, a.ResolveRefs(context.Background(), nil))
}

// -----------------------------------------------------------------------------
// DeploymentSpec
// -----------------------------------------------------------------------------

func TestDeploymentValidate_OK(t *testing.T) {
	d := &Deployment{
		Metadata: ObjectMeta{Namespace: "default", Name: "prod", Version: "v1"},
		Spec: DeploymentSpec{
			TargetRef:    ResourceRef{Kind: KindAgent, Name: "alice", Version: "v1"},
			ProviderRef:  ResourceRef{Kind: KindProvider, Name: "local", Version: "v1"},
			DesiredState: DesiredStateDeployed,
		},
	}
	require.NoError(t, d.Validate())
}

func TestDeploymentValidate_RejectsBadTargetKind(t *testing.T) {
	d := &Deployment{
		Metadata: ObjectMeta{Namespace: "default", Name: "prod", Version: "v1"},
		Spec: DeploymentSpec{
			TargetRef:   ResourceRef{Kind: KindSkill, Name: "skill", Version: "v1"},
			ProviderRef: ResourceRef{Kind: KindProvider, Name: "local", Version: "v1"},
		},
	}
	paths := failedFields(t, d.Validate())
	require.Contains(t, paths, "spec.targetRef.kind")
}

func TestDeploymentValidate_RejectsBadProviderKind(t *testing.T) {
	d := &Deployment{
		Metadata: ObjectMeta{Namespace: "default", Name: "prod", Version: "v1"},
		Spec: DeploymentSpec{
			TargetRef:   ResourceRef{Kind: KindAgent, Name: "alice", Version: "v1"},
			ProviderRef: ResourceRef{Kind: KindAgent, Name: "nope", Version: "v1"},
		},
	}
	paths := failedFields(t, d.Validate())
	require.Contains(t, paths, "spec.providerRef.kind")
}

func TestDeploymentValidate_RejectsBadDesiredState(t *testing.T) {
	d := &Deployment{
		Metadata: ObjectMeta{Namespace: "default", Name: "prod", Version: "v1"},
		Spec: DeploymentSpec{
			TargetRef:    ResourceRef{Kind: KindAgent, Name: "alice", Version: "v1"},
			ProviderRef:  ResourceRef{Kind: KindProvider, Name: "local", Version: "v1"},
			DesiredState: "running",
		},
	}
	paths := failedFields(t, d.Validate())
	require.Contains(t, paths, "spec.desiredState")
}

func TestDeploymentResolveRefs_InheritsNamespace(t *testing.T) {
	var seen []ResourceRef
	resolver := func(ctx context.Context, ref ResourceRef) error {
		seen = append(seen, ref)
		return nil
	}
	d := &Deployment{
		Metadata: ObjectMeta{Namespace: "team-b", Name: "prod", Version: "v1"},
		Spec: DeploymentSpec{
			TargetRef:   ResourceRef{Kind: KindAgent, Name: "alice", Version: "v1"},
			ProviderRef: ResourceRef{Kind: KindProvider, Name: "local", Version: "v1"},
		},
	}
	require.NoError(t, d.ResolveRefs(context.Background(), resolver))
	require.Len(t, seen, 2)
	require.Equal(t, "team-b", seen[0].Namespace)
	require.Equal(t, "team-b", seen[1].Namespace)
}

// -----------------------------------------------------------------------------
// ProviderSpec
// -----------------------------------------------------------------------------

func TestProviderValidate_OK(t *testing.T) {
	p := &Provider{
		Metadata: ObjectMeta{Namespace: "default", Name: "local", Version: "v1"},
		Spec:     ProviderSpec{Platform: PlatformLocal},
	}
	require.NoError(t, p.Validate())
}

func TestProviderValidate_RejectsUnknownPlatform(t *testing.T) {
	p := &Provider{
		Metadata: ObjectMeta{Namespace: "default", Name: "custom", Version: "v1"},
		Spec:     ProviderSpec{Platform: "heroku"},
	}
	err := p.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "heroku")
}

// -----------------------------------------------------------------------------
// MCPServer
// -----------------------------------------------------------------------------

func TestMCPServerValidate_OK(t *testing.T) {
	m := &MCPServer{
		Metadata: ObjectMeta{Namespace: "default", Name: "tools", Version: "v1"},
		Spec: MCPServerSpec{
			Title: "Tools",
			Packages: []MCPPackage{{
				RegistryType: "oci",
				Identifier:   "ghcr.io/example/mcp-tools:1.0.0",
				Transport:    MCPTransport{Type: "stdio"},
			}},
		},
	}
	require.NoError(t, m.Validate())
}

func TestMCPServerValidate_RejectsBadRemote(t *testing.T) {
	m := &MCPServer{
		Metadata: ObjectMeta{Namespace: "default", Name: "tools", Version: "v1"},
		Spec: MCPServerSpec{
			Remotes: []MCPTransport{
				{Type: "streamable-http"}, // missing URL
			},
		},
	}
	paths := failedFields(t, m.Validate())
	require.Contains(t, paths, "spec.remotes[0].url")
}

func TestMCPServerValidate_IconRequiresHTTPS(t *testing.T) {
	bad := "http://example.com/icon.svg"
	m := &MCPServer{
		Metadata: ObjectMeta{Namespace: "default", Name: "tools", Version: "v1"},
		Spec: MCPServerSpec{
			Icons: []MCPIcon{{Src: bad}},
		},
	}
	paths := failedFields(t, m.Validate())
	require.Contains(t, paths, "spec.icons[0].src")
}
