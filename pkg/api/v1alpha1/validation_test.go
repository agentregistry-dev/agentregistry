package v1alpha1

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
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

func TestValidateObjectMeta_RejectsNonDNSSubdomainName(t *testing.T) {
	// metadata.name across every kind must be DNS-1123 subdomain form.
	for _, bad := range []string{"ai.exa/exa", "UPPER", "has_underscore", "name with space", "-leading", "trailing-", "..double-dot", "trailing-dot."} {
		errs := ValidateObjectMeta(ObjectMeta{Namespace: "default", Name: bad})
		require.NotEmpty(t, errs, "name %q should be invalid", bad)
	}
}

func TestValidateObjectMeta_AcceptsDottedName(t *testing.T) {
	// DNS-1123 subdomain allows dot-separated segments (reverse-DNS style).
	for _, ok := range []string{"a", "foo", "foo-bar", "io.example", "io.example.app", "mcp.fetch.v1"} {
		errs := ValidateObjectMeta(ObjectMeta{Namespace: "default", Name: ok})
		require.Empty(t, errs, "name %q should be valid: %v", ok, errs)
	}
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

func TestAgentValidate_AcceptsRepositoryWithBranchAndCommit(t *testing.T) {
	a := &Agent{
		Metadata: ObjectMeta{Namespace: "default", Name: "a"},
		Spec: AgentSpec{
			Source: &AgentSource{
				Repository: &Repository{
					URL:    "https://github.com/example/repo",
					Branch: "feature/x",
					Commit: "abc1234def",
				},
			},
		},
	}
	require.NoError(t, a.Validate())
}

func TestAgentValidate_RejectsBranchOrCommitWithoutURL(t *testing.T) {
	cases := []struct {
		name string
		repo Repository
		want string
	}{
		{"branch without url", Repository{Branch: "feature/x"}, "spec.source.repository.branch"},
		{"commit without url", Repository{Commit: "abc1234"}, "spec.source.repository.commit"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := &Agent{
				Metadata: ObjectMeta{Namespace: "default", Name: "a"},
				Spec: AgentSpec{
					Source: &AgentSource{Repository: &tc.repo},
				},
			}
			paths := failedFields(t, a.Validate())
			require.Contains(t, paths, tc.want)
		})
	}
}

func TestAgentResolveRefs_OK(t *testing.T) {
	resolver := func(ctx context.Context, ref ResourceRef) error { return nil }
	a := &Agent{
		Metadata: ObjectMeta{Namespace: "default", Name: "a"},
		Spec: AgentSpec{
			MCPServers: []ResourceRef{{Kind: KindMCPServer, Name: "tools", Tag: "v1"}},
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
			RuntimeRef:   ResourceRef{Kind: KindRuntime, Name: "local"},
			DesiredState: DesiredStateDeployed,
		},
	}
	require.NoError(t, d.Validate())
}

func TestDeploymentValidate_RejectsBadTargetKind(t *testing.T) {
	d := &Deployment{
		Metadata: ObjectMeta{Namespace: "default", Name: "prod"},
		Spec: DeploymentSpec{
			TargetRef:  ResourceRef{Kind: KindSkill, Name: "skill", Tag: "stable"},
			RuntimeRef: ResourceRef{Kind: KindRuntime, Name: "local"},
		},
	}
	paths := failedFields(t, d.Validate())
	require.Contains(t, paths, "spec.targetRef.kind")
}

func TestDeploymentValidate_RejectsBadRuntimeKind(t *testing.T) {
	d := &Deployment{
		Metadata: ObjectMeta{Namespace: "default", Name: "prod"},
		Spec: DeploymentSpec{
			TargetRef:  ResourceRef{Kind: KindAgent, Name: "alice", Tag: "stable"},
			RuntimeRef: ResourceRef{Kind: KindAgent, Name: "nope"},
		},
	}
	paths := failedFields(t, d.Validate())
	require.Contains(t, paths, "spec.runtimeRef.kind")
}

func TestDeploymentValidate_RejectsBadDesiredState(t *testing.T) {
	d := &Deployment{
		Metadata: ObjectMeta{Namespace: "default", Name: "prod"},
		Spec: DeploymentSpec{
			TargetRef:    ResourceRef{Kind: KindAgent, Name: "alice", Tag: "stable"},
			RuntimeRef:   ResourceRef{Kind: KindRuntime, Name: "local"},
			DesiredState: "running",
		},
	}
	paths := failedFields(t, d.Validate())
	require.Contains(t, paths, "spec.desiredState")
}

func TestDeploymentValidate_DeploymentRefsOK(t *testing.T) {
	d := &Deployment{
		Metadata: ObjectMeta{Namespace: "default", Name: "agent-prod"},
		Spec: DeploymentSpec{
			TargetRef:    ResourceRef{Kind: KindAgent, Name: "alice", Tag: "stable"},
			RuntimeRef:   ResourceRef{Kind: KindRuntime, Name: "local"},
			DesiredState: DesiredStateDeployed,
			DeploymentRefs: []DeploymentRef{
				{Name: "weather-mcp-prod"},
				{Namespace: "tools", Name: "fs-mcp-prod"},
			},
		},
	}
	require.NoError(t, d.Validate())
}

func TestDeploymentValidate_DeploymentRefsRejectMissingName(t *testing.T) {
	d := &Deployment{
		Metadata: ObjectMeta{Namespace: "default", Name: "agent-prod"},
		Spec: DeploymentSpec{
			TargetRef:      ResourceRef{Kind: KindAgent, Name: "alice", Tag: "stable"},
			RuntimeRef:     ResourceRef{Kind: KindRuntime, Name: "local"},
			DeploymentRefs: []DeploymentRef{{Namespace: "tools"}}, // missing Name
		},
	}
	paths := failedFields(t, d.Validate())
	require.Contains(t, paths, "spec.deploymentRefs[0].name")
}

func TestDeploymentValidate_DeploymentRefsRejectBadNamespace(t *testing.T) {
	d := &Deployment{
		Metadata: ObjectMeta{Namespace: "default", Name: "agent-prod"},
		Spec: DeploymentSpec{
			TargetRef:      ResourceRef{Kind: KindAgent, Name: "alice", Tag: "stable"},
			RuntimeRef:     ResourceRef{Kind: KindRuntime, Name: "local"},
			DeploymentRefs: []DeploymentRef{{Namespace: "Bad NS", Name: "ok"}},
		},
	}
	paths := failedFields(t, d.Validate())
	require.Contains(t, paths, "spec.deploymentRefs[0].namespace")
}

// Deployment.spec.targetRef may omit tag; reference resolution treats blank as
// the literal "latest" tag.
func TestDeploymentValidate_AllowsEmptyTargetRefTag(t *testing.T) {
	d := &Deployment{
		Metadata: ObjectMeta{Namespace: "default", Name: "prod"},
		Spec: DeploymentSpec{
			TargetRef:  ResourceRef{Kind: KindAgent, Name: "alice"},
			RuntimeRef: ResourceRef{Kind: KindRuntime, Name: "local"},
		},
	}
	require.NoError(t, d.Validate())
}

func TestDeploymentValidate_RejectsBadTargetRefTag(t *testing.T) {
	d := &Deployment{
		Metadata: ObjectMeta{Namespace: "default", Name: "prod"},
		Spec: DeploymentSpec{
			TargetRef:  ResourceRef{Kind: KindAgent, Name: "alice", Tag: "bad tag"},
			RuntimeRef: ResourceRef{Kind: KindRuntime, Name: "local"},
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
			TargetRef:  ResourceRef{Kind: KindAgent, Name: "alice", Tag: "stable"},
			RuntimeRef: ResourceRef{Kind: KindRuntime, Name: "local"},
		},
	}
	require.NoError(t, d.ResolveRefs(context.Background(), resolver))
	require.Len(t, seen, 2)
	require.Equal(t, "team-b", seen[0].Namespace)
	require.Equal(t, "team-b", seen[1].Namespace)
}

// -----------------------------------------------------------------------------
// Runtime
// -----------------------------------------------------------------------------

func TestRuntimeValidate_OK(t *testing.T) {
	r := &Runtime{
		Metadata: ObjectMeta{Namespace: "default", Name: "local"},
		Spec:     RuntimeSpec{Type: TypeLocal},
	}
	require.NoError(t, r.Validate())
}

func TestRuntimeValidate_RejectsUnknownType(t *testing.T) {
	r := &Runtime{
		Metadata: ObjectMeta{Namespace: "default", Name: "custom"},
		Spec:     RuntimeSpec{Type: "heroku"},
	}
	err := r.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "heroku")
}

// TestRuntimeValidate_CanonicalizesType ensures Validate rewrites
// Spec.Type to its canonical CamelCase form regardless of input casing.
// Downstream adapter dispatch relies on exact-match equality, so the
// case-insensitive normalization MUST land at admission.
func TestRuntimeValidate_CanonicalizesType(t *testing.T) {
	for _, input := range []string{"local", "LOCAL", "Local", " Local "} {
		r := &Runtime{
			Metadata: ObjectMeta{Namespace: "default", Name: "x"},
			Spec:     RuntimeSpec{Type: input},
		}
		require.NoError(t, r.Validate(), "input %q should validate", input)
		require.Equal(t, TypeLocal, r.Spec.Type,
			"input %q should canonicalize to %q, got %q", input, TypeLocal, r.Spec.Type)
	}
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
					ServerName:   "mcp-tools",
				},
			},
		},
	}
	require.NoError(t, m.Validate())
}

func TestMCPServerValidate_RejectsBadRemote(t *testing.T) {
	r := &MCPServer{
		Metadata: ObjectMeta{Namespace: "default", Name: "tools", Tag: "v1"},
		Spec: MCPServerSpec{
			Remote: &MCPRemote{Type: "streamable-http"}, // missing URL
		},
	}
	paths := failedFields(t, r.Validate())
	require.Contains(t, paths, "spec.remote.url")
}

func TestMCPServerValidate_RemoteAndSourceMutuallyExclusive(t *testing.T) {
	m := &MCPServer{
		Metadata: ObjectMeta{Namespace: "default", Name: "tools", Tag: "v1"},
		Spec: MCPServerSpec{
			Source: &MCPServerSource{
				Package: &MCPPackage{
					RegistryType: "oci",
					Identifier:   "ghcr.io/example/mcp-tools:1.0.0",
					Transport:    MCPTransport{Type: "stdio"},
					ServerName:   "mcp-tools",
				},
			},
			Remote: &MCPRemote{Type: "streamable-http", URL: "https://example.test/mcp"},
		},
	}
	paths := failedFields(t, m.Validate())
	require.Contains(t, paths, "spec")
}

func TestMCPServerValidate_RequiresSourceOrRemote(t *testing.T) {
	m := &MCPServer{
		Metadata: ObjectMeta{Namespace: "default", Name: "tools", Tag: "v1"},
		Spec:     MCPServerSpec{},
	}
	paths := failedFields(t, m.Validate())
	require.Contains(t, paths, "spec")
}

func TestMCPServerValidate_HTTPPortRange(t *testing.T) {
	mk := func(port uint16) *MCPServer {
		return &MCPServer{
			Metadata: ObjectMeta{Name: "x"},
			Spec: MCPServerSpec{
				Source: &MCPServerSource{
					Package: &MCPPackage{
						RegistryType: "oci",
						Identifier:   "img:latest",
						Transport:    MCPTransport{Type: "http", Port: port},
						ServerName:   "x",
					},
				},
			},
		}
	}
	const portPath = "spec.source.package.transport.port"
	require.Contains(t, failedFields(t, mk(0).Validate()), portPath, "http with port 0 must fail")
	require.NotContains(t, failedFields(t, mk(8080).Validate()), portPath, "http with a valid port must pass the port check")
}

func mcpOpenAPIServer(url, schema string) *MCPServer {
	return &MCPServer{
		Metadata: ObjectMeta{Namespace: "default", Name: "petstore", Tag: "v1"},
		Spec: MCPServerSpec{
			OpenAPI: &MCPServerOpenAPI{URL: url, Schema: schema},
		},
	}
}

const validOpenAPISchema = `{"openapi":"3.0.0","info":{"title":"x","version":"1.0.0"},"paths":{}}`

func TestMCPServerValidate_OpenAPI_OK(t *testing.T) {
	m := mcpOpenAPIServer("https://petstore.default.svc.cluster.local:8443", validOpenAPISchema)
	require.NoError(t, m.Validate())
}

func TestMCPServerValidate_OpenAPI_RequiresURLAndSchema(t *testing.T) {
	paths := failedFields(t, mcpOpenAPIServer("", "").Validate())
	require.Contains(t, paths, "spec.openapi.url")
	require.Contains(t, paths, "spec.openapi.schema")
}

func TestMCPServerValidate_OpenAPI_RejectsURLWithPathQueryOrScheme(t *testing.T) {
	require.Contains(t, failedFields(t, mcpOpenAPIServer("https://host:8443/v1", validOpenAPISchema).Validate()), "spec.openapi.url",
		"url with a path must fail")
	require.Contains(t, failedFields(t, mcpOpenAPIServer("https://host:8443?a=b", validOpenAPISchema).Validate()), "spec.openapi.url",
		"url with a query must fail")
	require.Contains(t, failedFields(t, mcpOpenAPIServer("ftp://host", validOpenAPISchema).Validate()), "spec.openapi.url",
		"non-http(s) scheme must fail")
	require.Contains(t, failedFields(t, mcpOpenAPIServer("http://:8080", validOpenAPISchema).Validate()), "spec.openapi.url",
		"a port with no host must fail")
	require.Contains(t, failedFields(t, mcpOpenAPIServer("http://user:pass@host", validOpenAPISchema).Validate()), "spec.openapi.url",
		"userinfo must fail")
	require.NotContains(t, failedFields(t, mcpOpenAPIServer("http://host:8080/", validOpenAPISchema).Validate()), "spec.openapi.url",
		"a bare trailing slash is allowed")
	require.NotContains(t, failedFields(t, mcpOpenAPIServer("https://host", validOpenAPISchema).Validate()), "spec.openapi.url",
		"a host without a port is allowed")
	require.NotContains(t, failedFields(t, mcpOpenAPIServer("https://host:8443", validOpenAPISchema).Validate()), "spec.openapi.url",
		"a host with a port is allowed")
}

func TestMCPServerValidate_OpenAPI_RejectsBadSchema(t *testing.T) {
	require.Contains(t, failedFields(t, mcpOpenAPIServer("https://host", "not json").Validate()), "spec.openapi.schema",
		"non-JSON schema must fail")
	require.Contains(t, failedFields(t, mcpOpenAPIServer("https://host", `{"paths":{}}`).Validate()), "spec.openapi.schema",
		"JSON without a top-level openapi field must fail")
	require.Contains(t, failedFields(t, mcpOpenAPIServer("https://host", `{"openapi":3}`).Validate()), "spec.openapi.schema",
		"a non-string openapi field must fail")
	require.Contains(t, failedFields(t, mcpOpenAPIServer("https://host", `{"openapi":"2.0"}`).Validate()), "spec.openapi.schema",
		"a non-3.x openapi version must fail")
	require.NotContains(t, failedFields(t, mcpOpenAPIServer("https://host", `{"openapi":"3.1.0"}`).Validate()), "spec.openapi.schema",
		"an OpenAPI 3.1 document is allowed")
}

func TestMCPServerValidate_OpenAPI_MutuallyExclusive(t *testing.T) {
	m := mcpOpenAPIServer("https://host", validOpenAPISchema)
	m.Spec.Remote = &MCPRemote{Type: "streamable-http", URL: "https://example.test/mcp"}
	require.Contains(t, failedFields(t, m.Validate()), "spec")
}

func TestValidateNameField(t *testing.T) {
	maxLabelLen := strings.Repeat("a", 63) // single segment at max label length
	tooLongSegment := strings.Repeat("a", 64)
	// 253-char dotted subdomain at the upper bound (4 segments of 63 + 3 dots = 255 → 3 segments of 63 + 1 of 61 + 3 dots = 253)
	maxSubdomain := strings.Repeat("a", 63) + "." + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 61)
	tooLongSubdomain := maxSubdomain + "x"

	testCases := []struct {
		label     string
		name      string
		expectErr bool
	}{
		// positives
		{"single char", "a", false},
		{"lowercase only", "myserver", false},
		{"contains hyphen", "my-server", false},
		{"single dot-separated segment at label max", maxLabelLen, false},
		{"dotted (reverse-DNS)", "io.example", false},
		{"deeply dotted", "mcp.fetch.v1", false},
		{"hyphen plus dot", "my-server.io", false},
		{"max-length subdomain", maxSubdomain, false},

		// negatives
		{"empty", "", true},
		{"contains uppercase", "MyServer", true},
		{"contains underscore", "my_server", true},
		{"contains slash", "example/server", true},
		{"leading hyphen", "-server", true},
		{"trailing hyphen", "server-", true},
		{"leading dot", ".server", true},
		{"trailing dot", "server.", true},
		{"double dot", "foo..bar", true},
		{"segment longer than 63 chars", tooLongSegment, true},
		{"total longer than 253 chars", tooLongSubdomain, true},
		{"single hyphen", "-", true},
		{"non-ascii", "café", true},
		{"contains space", "my server", true},
	}
	for _, c := range testCases {
		t.Run(c.label, func(t *testing.T) {
			err := validateNameField(c.name)
			if c.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateNameField_ErrorMessage(t *testing.T) {
	// Format errors should mention DNS-1123 so operators can self-diagnose.
	err := validateNameField("io.example/foo")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidFormat)
	assert.Contains(t, err.Error(), "DNS-1123 subdomain")

	// Required-field errors should be the standard sentinel.
	err = validateNameField("")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrRequiredField)
}

func TestValidateMCPPackageName(t *testing.T) {
	maxLenStr := strings.Repeat("a", 100) + "/" + strings.Repeat("b", 99)   // 200 chars, exact max
	longLenStr := strings.Repeat("a", 100) + "/" + strings.Repeat("b", 101) // 202 chars

	testCases := []struct {
		label     string
		name      string
		expectErr bool
	}{
		{"empty (caught by required check elsewhere)", "", false},
		{"single segment", "my-mcp", false},
		{"dotted single segment", "io.example.mcp", false},
		{"underscore in single segment", "my_mcp", false},
		{"multi-period dns name", "io.github.user/server", false},
		{"follows dns naming scheme", "com.example/foo", false},
		{"contains multiple special ascii chars", "foo.bar-baz/my_server", false},
		{"multiple slashes", "a/b/c", false},
		{"within max len", maxLenStr, false},
		{"single char", "a", false},
		{"too long", longLenStr, true},
		{"non-ascii", "café/foo", true},
		{"contains space", "io.example/my server", true},
		{"contains colon", "io.example:foo", true},
		{"contains exclamation", "foo!bar", true},
	}
	for _, c := range testCases {
		t.Run(c.label, func(t *testing.T) {
			err := validateMCPPackageName(c.name)
			if c.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestMCPServerValidate_MCPPackageName_RejectsBadFormat(t *testing.T) {
	m := &MCPServer{
		TypeMeta: TypeMeta{APIVersion: "ar.dev/v1alpha1", Kind: KindMCPServer},
		Metadata: ObjectMeta{Namespace: "default", Name: "my-server"},
		Spec: MCPServerSpec{
			Source: &MCPServerSource{
				Package: &MCPPackage{
					RegistryType: "npm",
					Identifier:   "my-pkg",
					Transport:    MCPTransport{Type: "stdio"},
					ServerName:   "has space", // invalid: spaces not allowed
				},
			},
		},
	}
	err := m.Validate()
	require.Error(t, err)
	paths := failedFields(t, err)
	require.Contains(t, paths, "spec.source.package.serverName")
}

func TestMCPServerValidate_MCPPackageName_AcceptsValid(t *testing.T) {
	m := &MCPServer{
		TypeMeta: TypeMeta{APIVersion: "ar.dev/v1alpha1", Kind: KindMCPServer},
		Metadata: ObjectMeta{Namespace: "default", Name: "my-server"},
		Spec: MCPServerSpec{
			Source: &MCPServerSource{
				Package: &MCPPackage{
					RegistryType: "pypi",
					Identifier:   "mcp-server-fetch",
					Transport:    MCPTransport{Type: "stdio"},
					ServerName:   "io.github.modelcontextprotocol/server-fetch",
				},
			},
		},
	}
	require.NoError(t, m.Validate())
}

func TestMCPServerValidate_ServerName_RequiredForNonMCPB(t *testing.T) {
	for _, rt := range []string{"npm", "pypi", "oci", "nuget"} {
		t.Run(rt, func(t *testing.T) {
			m := &MCPServer{
				TypeMeta: TypeMeta{APIVersion: "ar.dev/v1alpha1", Kind: KindMCPServer},
				Metadata: ObjectMeta{Namespace: "default", Name: "my-server"},
				Spec: MCPServerSpec{
					Source: &MCPServerSource{
						Package: &MCPPackage{
							RegistryType: rt,
							Identifier:   "my-pkg",
							Transport:    MCPTransport{Type: "stdio"},
							// ServerName intentionally omitted
						},
					},
				},
			}
			err := m.Validate()
			require.Error(t, err)
			paths := failedFields(t, err)
			require.Contains(t, paths, "spec.source.package.serverName")
		})
	}
}

func TestMCPServerValidate_ServerName_OptionalForMCPB(t *testing.T) {
	m := &MCPServer{
		TypeMeta: TypeMeta{APIVersion: "ar.dev/v1alpha1", Kind: KindMCPServer},
		Metadata: ObjectMeta{Namespace: "default", Name: "my-server"},
		Spec: MCPServerSpec{
			Source: &MCPServerSource{
				Package: &MCPPackage{
					RegistryType: RegistryTypeMCPB,
					Identifier:   "https://example.com/pkg.mcpb",
					Transport:    MCPTransport{Type: "stdio"},
					// ServerName intentionally omitted — MCPB has no ownership check
				},
			},
		},
	}
	require.NoError(t, m.Validate())
}

func TestMCPServerValidateRegistries_UsesServerNameWhenSet(t *testing.T) {
	var gotClaim string
	validator := func(_ context.Context, _ RegistryPackage, claim string) error {
		gotClaim = claim
		return nil
	}
	m := &MCPServer{
		Metadata: ObjectMeta{Namespace: "default", Name: "my-server"},
		Spec: MCPServerSpec{Source: &MCPServerSource{Package: &MCPPackage{
			RegistryType: "pypi", Identifier: "mcp-server-fetch", Transport: MCPTransport{Type: "stdio"},
			ServerName: "io.github.modelcontextprotocol/server-fetch",
		}}},
	}
	require.NoError(t, m.ValidateRegistries(context.Background(), validator))
	require.Equal(t, "io.github.modelcontextprotocol/server-fetch", gotClaim)
}
