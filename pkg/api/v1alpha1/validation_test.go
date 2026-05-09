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
	m := ObjectMeta{Namespace: "default", Name: "alice"}
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
}

func TestValidateObjectMeta_RejectsBadNamespace(t *testing.T) {
	for _, bad := range []string{"UPPER", "has spaces", "has_underscore", "ai.exa/exa", "-leading", "trailing-"} {
		errs := ValidateObjectMeta(ObjectMeta{Namespace: bad, Name: "x"})
		require.NotEmpty(t, errs, "namespace %q should be invalid", bad)
	}
}

func TestValidateObjectMeta_AcceptsDNSStyleName(t *testing.T) {
	// Names can carry slashes (dns-like). Namespaces cannot.
	errs := ValidateObjectMeta(ObjectMeta{Namespace: "default", Name: "ai.exa/exa"})
	require.Empty(t, errs)
}

func TestValidateObjectMeta_RejectsBadLabelKey(t *testing.T) {
	errs := ValidateObjectMeta(ObjectMeta{
		Namespace: "default", Name: "x",
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
		Metadata: ObjectMeta{Namespace: "default", Name: "alice"},
		Spec: AgentSpec{
			Title: "Alice",
			MCPServers: []ResourceRef{
				{Kind: KindMCPServer, Name: "tools", Tag: "v1"},
			},
		},
	}
	require.NoError(t, a.Validate())
}

func TestAgentValidate_RejectsWrongRefKind(t *testing.T) {
	a := &Agent{
		Metadata: ObjectMeta{Namespace: "default", Name: "a"},
		Spec: AgentSpec{
			MCPServers: []ResourceRef{{Kind: KindSkill, Name: "wrong", Tag: "v1"}},
		},
	}
	paths := failedFields(t, a.Validate())
	require.Contains(t, paths, "spec.mcpServers[0].kind")
}

func TestAgentValidate_AcceptsBlankOptionalFields(t *testing.T) {
	a := &Agent{
		Metadata: ObjectMeta{Namespace: "default", Name: "minimal"},
		Spec:     AgentSpec{}, // everything blank
	}
	require.NoError(t, a.Validate())
}

func TestAgentValidate_AccumulatesErrors(t *testing.T) {
	a := &Agent{
		Metadata: ObjectMeta{Namespace: "default", Name: "a"},
		Spec: AgentSpec{
			Title: "   ", // whitespace only
		},
	}
	paths := failedFields(t, a.Validate())
	require.Contains(t, paths, "spec.title")
}

func TestAgentResolveRefs_OK(t *testing.T) {
	resolver := func(ctx context.Context, ref ResourceRef) error { return nil }
	a := &Agent{
		Metadata: ObjectMeta{Namespace: "default", Name: "a"},
		Spec: AgentSpec{
			MCPServers: []ResourceRef{{Kind: KindMCPServer, Name: "tools", Tag: "v1"}},
			Skills:     []ResourceRef{{Kind: KindSkill, Name: "code-review", Tag: "v1"}},
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
		Metadata: ObjectMeta{Namespace: "default", Name: "a", Tag: "v1"},
		Spec: AgentSpec{
			MCPServers: []ResourceRef{
				{Kind: KindMCPServer, Name: "tools", Tag: "v1"},
				{Kind: KindMCPServer, Name: "missing", Tag: "v1"},
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
		Metadata: ObjectMeta{Namespace: "team-a", Name: "a", Tag: "v1"},
		Spec: AgentSpec{
			MCPServers: []ResourceRef{
				// blank namespace should inherit Agent's "team-a"
				{Kind: KindMCPServer, Name: "local-tools", Tag: "v1"},
				// explicit namespace sticks
				{Kind: KindMCPServer, Namespace: "shared", Name: "common", Tag: "v1"},
			},
		},
	}
	require.NoError(t, a.ResolveRefs(context.Background(), resolver))
	require.Len(t, seen, 2)
	require.Equal(t, "team-a", seen[0].Namespace)
	require.Equal(t, "shared", seen[1].Namespace)
}

func TestAgentResolveRefs_NilResolverIsNoOp(t *testing.T) {
	a := &Agent{Metadata: ObjectMeta{Namespace: "default", Name: "a"}}
	require.NoError(t, a.ResolveRefs(context.Background(), nil))
}

// -----------------------------------------------------------------------------
// DeploymentSpec
// -----------------------------------------------------------------------------

func TestDeploymentValidate_OK(t *testing.T) {
	d := &Deployment{
		Metadata: ObjectMeta{Namespace: "default", Name: "prod"},
		Spec: DeploymentSpec{
			TargetRef:    ResourceRef{Kind: KindAgent, Name: "alice", Tag: "stable"},
			ProviderRef:  ResourceRef{Kind: KindProvider, Name: "local"},
			DesiredState: DesiredStateDeployed,
		},
	}
	require.NoError(t, d.Validate())
}

func TestDeploymentValidate_RejectsBadTargetKind(t *testing.T) {
	d := &Deployment{
		Metadata: ObjectMeta{Namespace: "default", Name: "prod"},
		Spec: DeploymentSpec{
			TargetRef:   ResourceRef{Kind: KindSkill, Name: "skill", Tag: "stable"},
			ProviderRef: ResourceRef{Kind: KindProvider, Name: "local"},
		},
	}
	paths := failedFields(t, d.Validate())
	require.Contains(t, paths, "spec.targetRef.kind")
}

func TestDeploymentValidate_RejectsBadProviderKind(t *testing.T) {
	d := &Deployment{
		Metadata: ObjectMeta{Namespace: "default", Name: "prod"},
		Spec: DeploymentSpec{
			TargetRef:   ResourceRef{Kind: KindAgent, Name: "alice", Tag: "stable"},
			ProviderRef: ResourceRef{Kind: KindAgent, Name: "nope"},
		},
	}
	paths := failedFields(t, d.Validate())
	require.Contains(t, paths, "spec.providerRef.kind")
}

func TestDeploymentValidate_RejectsBadDesiredState(t *testing.T) {
	d := &Deployment{
		Metadata: ObjectMeta{Namespace: "default", Name: "prod"},
		Spec: DeploymentSpec{
			TargetRef:    ResourceRef{Kind: KindAgent, Name: "alice", Tag: "stable"},
			ProviderRef:  ResourceRef{Kind: KindProvider, Name: "local"},
			DesiredState: "running",
		},
	}
	paths := failedFields(t, d.Validate())
	require.Contains(t, paths, "spec.desiredState")
}

// Deployment.spec.targetRef may omit tag; reference resolution treats blank as
// the literal "latest" tag.
func TestDeploymentValidate_AllowsEmptyTargetRefTag(t *testing.T) {
	d := &Deployment{
		Metadata: ObjectMeta{Namespace: "default", Name: "prod"},
		Spec: DeploymentSpec{
			TargetRef:   ResourceRef{Kind: KindAgent, Name: "alice"},
			ProviderRef: ResourceRef{Kind: KindProvider, Name: "local"},
		},
	}
	require.NoError(t, d.Validate())
}

func TestDeploymentValidate_RejectsBadTargetRefTag(t *testing.T) {
	d := &Deployment{
		Metadata: ObjectMeta{Namespace: "default", Name: "prod"},
		Spec: DeploymentSpec{
			TargetRef:   ResourceRef{Kind: KindAgent, Name: "alice", Tag: "bad tag"},
			ProviderRef: ResourceRef{Kind: KindProvider, Name: "local"},
		},
	}
	paths := failedFields(t, d.Validate())
	require.Contains(t, paths, "spec.targetRef.tag")
}

func TestDeploymentResolveRefs_InheritsNamespace(t *testing.T) {
	var seen []ResourceRef
	resolver := func(ctx context.Context, ref ResourceRef) error {
		seen = append(seen, ref)
		return nil
	}
	d := &Deployment{
		Metadata: ObjectMeta{Namespace: "team-b", Name: "prod"},
		Spec: DeploymentSpec{
			TargetRef:   ResourceRef{Kind: KindAgent, Name: "alice", Tag: "stable"},
			ProviderRef: ResourceRef{Kind: KindProvider, Name: "local"},
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
		Metadata: ObjectMeta{Namespace: "default", Name: "local"},
		Spec:     ProviderSpec{Platform: PlatformLocal},
	}
	require.NoError(t, p.Validate())
}

func TestProviderValidate_RejectsUnknownPlatform(t *testing.T) {
	p := &Provider{
		Metadata: ObjectMeta{Namespace: "default", Name: "custom"},
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
		Metadata: ObjectMeta{Namespace: "default", Name: "tools", Tag: "v1"},
		Spec: MCPServerSpec{
			Title: "Tools",
			Source: &MCPServerSource{
				Package: &MCPPackage{
					RegistryType: "oci",
					Identifier:   "ghcr.io/example/mcp-tools:1.0.0",
					Transport:    MCPTransport{Type: "stdio"},
				},
			},
		},
	}
	require.NoError(t, m.Validate())
}

func TestRemoteMCPServerValidate_RejectsBadRemote(t *testing.T) {
	r := &RemoteMCPServer{
		Metadata: ObjectMeta{Namespace: "default", Name: "tools", Tag: "v1"},
		Spec: RemoteMCPServerSpec{
			Remote: MCPTransport{Type: "streamable-http"}, // missing URL
		},
	}
	paths := failedFields(t, r.Validate())
	require.Contains(t, paths, "spec.remote.url")
}
