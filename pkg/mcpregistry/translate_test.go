package mcpregistry_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/mcpregistry"
)

func TestServerNameRoundTrip(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		resource  string
		wantName  string
	}{
		{"explicit namespace", "team-a", "my-server", "team-a/my-server"},
		{"empty namespace defaults", "", "my-server", "default/my-server"},
		{"dotted resource name", "default", "io.github.foo.bar", "default/io.github.foo.bar"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mcpregistry.ServerName(tt.namespace, tt.resource)
			assert.Equal(t, tt.wantName, got)

			ns, name, err := mcpregistry.ParseServerName(got)
			require.NoError(t, err)
			wantNS := tt.namespace
			if wantNS == "" {
				wantNS = v1alpha1.DefaultNamespace
			}
			assert.Equal(t, wantNS, ns)
			assert.Equal(t, tt.resource, name)
		})
	}
}

func TestParseServerName_Errors(t *testing.T) {
	for _, in := range []string{"", "no-slash", "/missing-namespace", "missing-name/", "too/many/slashes"} {
		t.Run(in, func(t *testing.T) {
			_, _, err := mcpregistry.ParseServerName(in)
			assert.Error(t, err)
		})
	}
}

func TestFromMCPServer(t *testing.T) {
	created := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 4, 2, 9, 30, 0, 0, time.UTC)

	base := func() *v1alpha1.MCPServer {
		return &v1alpha1.MCPServer{
			TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindMCPServer},
			Metadata: v1alpha1.ObjectMeta{
				Namespace: "team-a",
				Name:      "weather",
				Tag:       "latest",
				CreatedAt: created,
				UpdatedAt: updated,
			},
		}
	}

	tests := []struct {
		name   string
		mutate func(*v1alpha1.MCPServer)
		check  func(*testing.T, mcpregistry.ServerResponse)
	}{
		{
			name: "npm stdio package with launch args and env",
			mutate: func(s *v1alpha1.MCPServer) {
				s.Spec.Title = "Weather"
				s.Spec.Description = "Weather tools"
				s.Spec.Source = &v1alpha1.MCPServerSource{
					Package: &v1alpha1.MCPPackage{
						Origin: v1alpha1.MCPPackageOrigin{
							Type:       v1alpha1.MCPPackageOriginTypeNPM,
							Identifier: "weather-mcp",
							NPM:        &v1alpha1.MCPPackageOriginNPM{Version: "1.2.3", ServerName: "io.github.acme/weather"},
						},
						Launch: &v1alpha1.MCPPackageLaunch{
							Command: "npx",
							Args: []v1alpha1.MCPArgument{
								{Type: v1alpha1.MCPArgumentTypePositional, Value: "weather-mcp"},
								{Type: v1alpha1.MCPArgumentTypeNamed, Name: "--port", Value: "8080"},
							},
							Env: []v1alpha1.MCPKeyValueInput{
								{Name: "API_KEY", IsRequired: true},
							},
						},
						Transport: v1alpha1.MCPTransport{Type: "stdio"},
					},
				}
			},
			check: func(t *testing.T, r mcpregistry.ServerResponse) {
				assert.Equal(t, "team-a/weather", r.Server.Name)
				assert.Equal(t, "Weather", r.Server.Title)
				assert.Equal(t, "Weather tools", r.Server.Description)
				assert.Equal(t, "1.2.3", r.Server.Version)
				assert.Equal(t, mcpregistry.SchemaURL, r.Server.Schema)
				require.Len(t, r.Server.Packages, 1)
				p := r.Server.Packages[0]
				assert.Equal(t, "npm", p.RegistryType)
				assert.Equal(t, "weather-mcp", p.Identifier)
				assert.Equal(t, "1.2.3", p.Version)
				assert.Equal(t, "npx", p.RuntimeHint)
				assert.Equal(t, "stdio", p.Transport.Type)
				require.Len(t, p.PackageArguments, 2)
				assert.Equal(t, "positional", p.PackageArguments[0].Type)
				assert.Equal(t, "weather-mcp", p.PackageArguments[0].Value)
				assert.Equal(t, "named", p.PackageArguments[1].Type)
				assert.Equal(t, "--port", p.PackageArguments[1].Name)
				require.Len(t, p.EnvironmentVariables, 1)
				assert.Equal(t, "API_KEY", p.EnvironmentVariables[0].Name)
				assert.True(t, p.EnvironmentVariables[0].IsRequired)
				// _meta is populated from server-managed metadata.
				require.NotNil(t, r.Meta)
				require.NotNil(t, r.Meta.Official)
				assert.Equal(t, "active", r.Meta.Official.Status)
				assert.True(t, r.Meta.Official.IsLatest)
				assert.Equal(t, created.Format(time.RFC3339), r.Meta.Official.PublishedAt)
				assert.Equal(t, updated.Format(time.RFC3339), r.Meta.Official.UpdatedAt)
			},
		},
		{
			name: "oci package derives version from identifier and http transport",
			mutate: func(s *v1alpha1.MCPServer) {
				s.Spec.Description = "containerized"
				s.Spec.Source = &v1alpha1.MCPServerSource{
					Package: &v1alpha1.MCPPackage{
						Origin: v1alpha1.MCPPackageOrigin{
							Type:       v1alpha1.MCPPackageOriginTypeOCI,
							Identifier: "ghcr.io/acme/weather:2.0.1",
							OCI:        &v1alpha1.MCPPackageOriginOCI{ServerName: "io.github.acme/weather"},
						},
						Transport: v1alpha1.MCPTransport{Type: "http", Port: 9000, Path: "/mcp"},
					},
				}
			},
			check: func(t *testing.T, r mcpregistry.ServerResponse) {
				assert.Equal(t, "2.0.1", r.Server.Version)
				require.Len(t, r.Server.Packages, 1)
				p := r.Server.Packages[0]
				assert.Equal(t, "oci", p.RegistryType)
				assert.Equal(t, "2.0.1", p.Version)
				assert.Equal(t, "streamable-http", p.Transport.Type)
				assert.Equal(t, "http://localhost:9000/mcp", p.Transport.URL)
			},
		},
		{
			name: "pypi mirror maps to registryBaseUrl",
			mutate: func(s *v1alpha1.MCPServer) {
				s.Spec.Description = "py"
				s.Spec.Source = &v1alpha1.MCPServerSource{
					Package: &v1alpha1.MCPPackage{
						Origin: v1alpha1.MCPPackageOrigin{
							Type:       v1alpha1.MCPPackageOriginTypePyPI,
							Identifier: "weather-mcp",
							PyPI:       &v1alpha1.MCPPackageOriginPyPI{Version: "0.5.0", Mirror: "https://pypi.example.com/simple"},
						},
						Transport: v1alpha1.MCPTransport{Type: "stdio"},
					},
				}
			},
			check: func(t *testing.T, r mcpregistry.ServerResponse) {
				require.Len(t, r.Server.Packages, 1)
				assert.Equal(t, "pypi", r.Server.Packages[0].RegistryType)
				assert.Equal(t, "0.5.0", r.Server.Packages[0].Version)
				assert.Equal(t, "https://pypi.example.com/simple", r.Server.Packages[0].RegistryBaseURL)
			},
		},
		{
			name: "remote-only server maps to remotes with sse and headers",
			mutate: func(s *v1alpha1.MCPServer) {
				s.Spec.Description = "remote"
				s.Spec.Remote = &v1alpha1.MCPRemote{
					Type: "sse",
					URL:  "https://mcp.example.com/sse",
					Headers: []v1alpha1.HTTPHeader{
						{Name: "Authorization", Value: "Bearer x"},
					},
				}
			},
			check: func(t *testing.T, r mcpregistry.ServerResponse) {
				assert.Empty(t, r.Server.Packages)
				require.Len(t, r.Server.Remotes, 1)
				assert.Equal(t, "sse", r.Server.Remotes[0].Type)
				assert.Equal(t, "https://mcp.example.com/sse", r.Server.Remotes[0].URL)
				require.Len(t, r.Server.Remotes[0].Headers, 1)
				assert.Equal(t, "Authorization", r.Server.Remotes[0].Headers[0].Name)
			},
		},
		{
			name: "unknown remote type normalizes to streamable-http",
			mutate: func(s *v1alpha1.MCPServer) {
				s.Spec.Description = "remote"
				s.Spec.Remote = &v1alpha1.MCPRemote{Type: "http", URL: "https://mcp.example.com/mcp"}
			},
			check: func(t *testing.T, r mcpregistry.ServerResponse) {
				require.Len(t, r.Server.Remotes, 1)
				assert.Equal(t, "streamable-http", r.Server.Remotes[0].Type)
			},
		},
		{
			name: "repository infers github source and keeps subfolder",
			mutate: func(s *v1alpha1.MCPServer) {
				s.Spec.Description = "repo"
				s.Spec.Source = &v1alpha1.MCPServerSource{
					Repository: &v1alpha1.Repository{
						URL:       "https://github.com/acme/weather",
						Branch:    "main",
						Subfolder: "servers/weather",
					},
				}
			},
			check: func(t *testing.T, r mcpregistry.ServerResponse) {
				require.NotNil(t, r.Server.Repository)
				assert.Equal(t, "https://github.com/acme/weather", r.Server.Repository.URL)
				assert.Equal(t, "github", r.Server.Repository.Source)
				assert.Equal(t, "servers/weather", r.Server.Repository.Subfolder)
			},
		},
		{
			name: "empty description falls back to title",
			mutate: func(s *v1alpha1.MCPServer) {
				s.Spec.Title = "Fallback Title"
			},
			check: func(t *testing.T, r mcpregistry.ServerResponse) {
				assert.Equal(t, "Fallback Title", r.Server.Description)
			},
		},
		{
			name: "pinned non-latest tag is not marked latest and supplies version",
			mutate: func(s *v1alpha1.MCPServer) {
				s.Metadata.Tag = "1.4.0"
			},
			check: func(t *testing.T, r mcpregistry.ServerResponse) {
				assert.Equal(t, "1.4.0", r.Server.Version)
				require.NotNil(t, r.Meta)
				require.NotNil(t, r.Meta.Official)
				assert.False(t, r.Meta.Official.IsLatest)
			},
		},
		{
			name: "no version source falls back to placeholder and deletion marks status",
			mutate: func(s *v1alpha1.MCPServer) {
				del := updated
				s.Metadata.DeletionTimestamp = &del
			},
			check: func(t *testing.T, r mcpregistry.ServerResponse) {
				assert.Equal(t, "0.0.0", r.Server.Version)
				require.NotNil(t, r.Meta)
				require.NotNil(t, r.Meta.Official)
				assert.Equal(t, "deleted", r.Meta.Official.Status)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := base()
			tt.mutate(s)
			got := mcpregistry.FromMCPServer(s)
			tt.check(t, got)
		})
	}
}
