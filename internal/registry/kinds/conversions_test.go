package kinds_test

import (
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/registry/kinds"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// ToAgentJSON
// ---------------------------------------------------------------------------

func TestToAgentJSON(t *testing.T) {
	md := kinds.Metadata{Name: "my-agent", Version: "1.0.0"}
	spec := &kinds.AgentSpec{
		// AgentManifest inline fields
		Image:             "img:1",
		Language:          "python",
		Framework:         "langchain",
		ModelProvider:     "openai",
		ModelName:         "gpt-4",
		Description:       "desc",
		TelemetryEndpoint: "http://telemetry",
		// AgentJSON top-level fields
		Title:      "My Agent",
		WebsiteURL: "http://website",
		McpServers: []kinds.AgentMcpServer{
			{
				Type:                       "sse",
				Name:                       "s1",
				Image:                      "img",
				Build:                      "build",
				Command:                    "cmd",
				Args:                       []string{"a", "b"},
				Env:                        []string{"K=V"},
				URL:                        "http://x",
				Headers:                    map[string]string{"X-Hdr": "val"},
				RegistryURL:                "http://reg",
				RegistryServerName:         "srv",
				RegistryServerVersion:      "1.2",
				RegistryServerPreferRemote: true,
			},
		},
		Skills: []kinds.AgentSkillRef{
			{
				Name:                 "sk1",
				Image:                "sk-img",
				RegistryURL:          "http://sk-reg",
				RegistrySkillName:    "the-skill",
				RegistrySkillVersion: "2.0",
			},
		},
		Prompts: []kinds.AgentPromptRef{
			{
				Name:                  "p1",
				RegistryURL:           "http://p-reg",
				RegistryPromptName:    "the-prompt",
				RegistryPromptVersion: "3.0",
			},
		},
		Repository: &kinds.AgentRepository{
			URL:       "http://repo",
			Source:    "github",
			ID:        "repo-id",
			Subfolder: "sub",
		},
		Packages: []kinds.AgentPackageRef{
			{
				RegistryType: "npm",
				Identifier:   "pkg-id",
				Version:      "1.0.0",
				Transport: struct {
					Type string `yaml:"type" json:"type"`
				}{Type: "stdio"},
			},
		},
		Remotes: []kinds.AgentRemote{
			{Type: "sse", URL: "http://remote"},
		},
	}

	got := kinds.ToAgentJSON(md, spec)

	require.NotNil(t, got)

	// Metadata
	assert.Equal(t, "my-agent", got.Name)
	assert.Equal(t, "1.0.0", got.Version)

	// AgentManifest inline fields
	assert.Equal(t, "img:1", got.Image)
	assert.Equal(t, "python", got.Language)
	assert.Equal(t, "langchain", got.Framework)
	assert.Equal(t, "openai", got.ModelProvider)
	assert.Equal(t, "gpt-4", got.ModelName)
	assert.Equal(t, "desc", got.Description)
	assert.Equal(t, "http://telemetry", got.TelemetryEndpoint)

	// AgentJSON top-level
	assert.Equal(t, "My Agent", got.Title)
	assert.Equal(t, "active", got.Status)
	assert.Equal(t, "http://website", got.WebsiteURL)

	// McpServers
	require.Len(t, got.McpServers, 1)
	ms := got.McpServers[0]
	assert.Equal(t, "sse", ms.Type)
	assert.Equal(t, "s1", ms.Name)
	assert.Equal(t, "img", ms.Image)
	assert.Equal(t, "build", ms.Build)
	assert.Equal(t, "cmd", ms.Command)
	assert.Equal(t, []string{"a", "b"}, ms.Args)
	assert.Equal(t, []string{"K=V"}, ms.Env)
	assert.Equal(t, "http://x", ms.URL)
	assert.Equal(t, map[string]string{"X-Hdr": "val"}, ms.Headers)
	assert.Equal(t, "http://reg", ms.RegistryURL)
	assert.Equal(t, "srv", ms.RegistryServerName)
	assert.Equal(t, "1.2", ms.RegistryServerVersion)
	assert.True(t, ms.RegistryServerPreferRemote)

	// Skills
	require.Len(t, got.Skills, 1)
	sk := got.Skills[0]
	assert.Equal(t, "sk1", sk.Name)
	assert.Equal(t, "sk-img", sk.Image)
	assert.Equal(t, "http://sk-reg", sk.RegistryURL)
	assert.Equal(t, "the-skill", sk.RegistrySkillName)
	assert.Equal(t, "2.0", sk.RegistrySkillVersion)

	// Prompts
	require.Len(t, got.Prompts, 1)
	pr := got.Prompts[0]
	assert.Equal(t, "p1", pr.Name)
	assert.Equal(t, "http://p-reg", pr.RegistryURL)
	assert.Equal(t, "the-prompt", pr.RegistryPromptName)
	assert.Equal(t, "3.0", pr.RegistryPromptVersion)

	// Repository
	require.NotNil(t, got.Repository)
	assert.Equal(t, "http://repo", got.Repository.URL)
	assert.Equal(t, "github", got.Repository.Source)
	assert.Equal(t, "repo-id", got.Repository.ID)
	assert.Equal(t, "sub", got.Repository.Subfolder)

	// Packages
	require.Len(t, got.Packages, 1)
	pkg := got.Packages[0]
	assert.Equal(t, "npm", pkg.RegistryType)
	assert.Equal(t, "pkg-id", pkg.Identifier)
	assert.Equal(t, "1.0.0", pkg.Version)
	assert.Equal(t, "stdio", pkg.Transport.Type)

	// Remotes
	require.Len(t, got.Remotes, 1)
	assert.Equal(t, "sse", got.Remotes[0].Type)
	assert.Equal(t, "http://remote", got.Remotes[0].URL)
}

func TestToAgentJSON_NilRepository(t *testing.T) {
	md := kinds.Metadata{Name: "agent-no-repo", Version: "0.1.0"}
	spec := &kinds.AgentSpec{Description: "no repo"}

	got := kinds.ToAgentJSON(md, spec)

	require.NotNil(t, got)
	assert.Nil(t, got.Repository)
	assert.Empty(t, got.McpServers)
	assert.Empty(t, got.Skills)
	assert.Empty(t, got.Prompts)
	assert.Empty(t, got.Packages)
	assert.Empty(t, got.Remotes)
}

// ---------------------------------------------------------------------------
// ToSkillJSON
// ---------------------------------------------------------------------------

func TestToSkillJSON(t *testing.T) {
	md := kinds.Metadata{Name: "my-skill", Version: "2.0.0"}
	spec := &kinds.SkillSpec{
		Title:       "My Skill",
		Category:    "data",
		Description: "skill desc",
		WebsiteURL:  "http://skill-site",
		Repository: &kinds.SkillRepository{
			URL:    "http://skill-repo",
			Source: "github",
		},
		Packages: []kinds.SkillPackageRef{
			{
				RegistryType: "pypi",
				Identifier:   "my-pkg",
				Version:      "3.1.4",
				Transport: struct {
					Type string `yaml:"type" json:"type"`
				}{Type: "stdio"},
			},
		},
		Remotes: []kinds.SkillRemoteInfo{
			{URL: "http://skill-remote"},
		},
	}

	got := kinds.ToSkillJSON(md, spec)

	require.NotNil(t, got)
	assert.Equal(t, "my-skill", got.Name)
	assert.Equal(t, "2.0.0", got.Version)
	assert.Equal(t, "My Skill", got.Title)
	assert.Equal(t, "data", got.Category)
	assert.Equal(t, "skill desc", got.Description)
	assert.Equal(t, "http://skill-site", got.WebsiteURL)
	assert.Equal(t, "active", got.Status)

	require.NotNil(t, got.Repository)
	assert.Equal(t, "http://skill-repo", got.Repository.URL)
	assert.Equal(t, "github", got.Repository.Source)

	require.Len(t, got.Packages, 1)
	pkg := got.Packages[0]
	assert.Equal(t, "pypi", pkg.RegistryType)
	assert.Equal(t, "my-pkg", pkg.Identifier)
	assert.Equal(t, "3.1.4", pkg.Version)
	assert.Equal(t, "stdio", pkg.Transport.Type)

	require.Len(t, got.Remotes, 1)
	assert.Equal(t, "http://skill-remote", got.Remotes[0].URL)
}

func TestToSkillJSON_NilRepository(t *testing.T) {
	md := kinds.Metadata{Name: "bare-skill", Version: "1.0.0"}
	spec := &kinds.SkillSpec{Description: "bare"}

	got := kinds.ToSkillJSON(md, spec)

	require.NotNil(t, got)
	assert.Nil(t, got.Repository)
	assert.Empty(t, got.Packages)
	assert.Empty(t, got.Remotes)
}

// ---------------------------------------------------------------------------
// ToPromptJSON
// ---------------------------------------------------------------------------

func TestToPromptJSON(t *testing.T) {
	md := kinds.Metadata{Name: "my-prompt", Version: "1.2.3"}
	spec := &kinds.PromptSpec{
		Description: "prompt desc",
		Content:     "You are a helpful assistant.",
	}

	got := kinds.ToPromptJSON(md, spec)

	require.NotNil(t, got)
	assert.Equal(t, "my-prompt", got.Name)
	assert.Equal(t, "1.2.3", got.Version)
	assert.Equal(t, "prompt desc", got.Description)
	assert.Equal(t, "You are a helpful assistant.", got.Content)
}

// ---------------------------------------------------------------------------
// ToServerJSON
// ---------------------------------------------------------------------------

func TestToServerJSON(t *testing.T) {
	mimeType := "image/png"
	theme := "dark"

	md := kinds.Metadata{Name: "io.example/my-server", Version: "1.0.0"}
	spec := &kinds.MCPSpec{
		Schema:      "https://example.com/schema",
		Description: "server desc",
		Title:       "My MCP Server",
		WebsiteURL:  "http://server-site",
		Repository: &kinds.MCPRepository{
			URL:       "http://mcp-repo",
			Source:    "github",
			ID:        "mcp-repo-id",
			Subfolder: "mcp-sub",
		},
		Icons: []kinds.MCPIcon{
			{
				Src:      "https://example.com/icon.png",
				MimeType: &mimeType,
				Sizes:    []string{"48x48", "96x96"},
				Theme:    &theme,
			},
		},
		Packages: []kinds.MCPPackage{
			{
				RegistryType:    "npm",
				RegistryBaseURL: "https://registry.npmjs.org",
				Identifier:      "@example/server",
				Version:         "1.0.0",
				FileSHA256:      "abc123",
				RunTimeHint:     "npx",
				Transport: kinds.MCPTransport{
					Type: "stdio",
					URL:  "http://transport-url",
					Headers: []kinds.MCPKeyValueInput{
						{
							Name:        "Authorization",
							Description: "auth header",
							IsRequired:  true,
							Format:      "string",
							Value:       "Bearer {token}",
							IsSecret:    true,
							Default:     "default-val",
							Placeholder: "enter token",
							Choices:     []string{"a", "b"},
							Variables: map[string]kinds.MCPInputVariable{
								"token": {
									Description: "API token",
									IsRequired:  true,
									Format:      "string",
									Value:       "tok",
									IsSecret:    true,
									Default:     "deftoken",
									Placeholder: "placeholder",
									Choices:     []string{"x"},
								},
							},
						},
					},
				},
				RuntimeArguments: []kinds.MCPArgument{
					{
						Type:        "positional",
						Name:        "--port",
						ValueHint:   "8080",
						IsRepeated:  false,
						Description: "port arg",
						IsRequired:  true,
						Format:      "number",
						Value:       "8080",
						IsSecret:    false,
						Default:     "3000",
						Placeholder: "port number",
						Choices:     []string{"3000", "8080"},
						Variables: map[string]kinds.MCPInputVariable{
							"portVar": {
								Description: "port variable",
								IsRequired:  false,
								Format:      "number",
								Value:       "8080",
								IsSecret:    false,
								Default:     "3000",
								Placeholder: "ph",
								Choices:     []string{"3000"},
							},
						},
					},
				},
				PackageArguments: []kinds.MCPArgument{
					{
						Type:        "named",
						Name:        "--config",
						ValueHint:   "config.json",
						IsRepeated:  true,
						Description: "config arg",
						IsRequired:  false,
						Format:      "filepath",
						Value:       "/etc/config.json",
						IsSecret:    false,
						Default:     "/etc/default.json",
						Placeholder: "path to config",
						Choices:     []string{"/etc/a.json"},
						Variables: map[string]kinds.MCPInputVariable{
							"configVar": {
								Description: "config variable",
								IsRequired:  true,
								Format:      "filepath",
								Value:       "/etc/config.json",
								IsSecret:    false,
								Default:     "/etc/default.json",
								Placeholder: "ph",
								Choices:     []string{"/a"},
							},
						},
					},
				},
				EnvironmentVariables: []kinds.MCPKeyValueInput{
					{
						Name:        "API_KEY",
						Description: "API key env var",
						IsRequired:  true,
						Format:      "string",
						Value:       "key-{secret}",
						IsSecret:    true,
						Default:     "default-key",
						Placeholder: "enter key",
						Choices:     []string{"k1"},
						Variables: map[string]kinds.MCPInputVariable{
							"secret": {
								Description: "the secret",
								IsRequired:  true,
								Format:      "string",
								Value:       "s",
								IsSecret:    true,
								Default:     "d",
								Placeholder: "p",
								Choices:     []string{"c"},
							},
						},
					},
				},
			},
		},
		Remotes: []kinds.MCPTransport{
			{
				Type: "sse",
				URL:  "http://remote-sse",
				Headers: []kinds.MCPKeyValueInput{
					{
						Name:        "X-Token",
						Description: "remote header",
						IsRequired:  false,
						Format:      "string",
						Value:       "tok",
						IsSecret:    true,
						Default:     "def",
						Placeholder: "ph",
						Choices:     []string{"t1"},
						Variables: map[string]kinds.MCPInputVariable{
							"v1": {
								Description: "var1",
								IsRequired:  true,
								Format:      "string",
								Value:       "val",
								IsSecret:    false,
								Default:     "d",
								Placeholder: "p",
								Choices:     []string{"c"},
							},
						},
					},
				},
			},
		},
	}

	got := kinds.ToServerJSON(md, spec)

	require.NotNil(t, got)

	// Top-level fields
	assert.Equal(t, "https://example.com/schema", got.Schema)
	assert.Equal(t, "io.example/my-server", got.Name)
	assert.Equal(t, "1.0.0", got.Version)
	assert.Equal(t, "server desc", got.Description)
	assert.Equal(t, "My MCP Server", got.Title)
	assert.Equal(t, "http://server-site", got.WebsiteURL)

	// Repository
	require.NotNil(t, got.Repository)
	assert.Equal(t, "http://mcp-repo", got.Repository.URL)
	assert.Equal(t, "github", got.Repository.Source)
	assert.Equal(t, "mcp-repo-id", got.Repository.ID)
	assert.Equal(t, "mcp-sub", got.Repository.Subfolder)

	// Icons
	require.Len(t, got.Icons, 1)
	ic := got.Icons[0]
	assert.Equal(t, "https://example.com/icon.png", ic.Src)
	require.NotNil(t, ic.MimeType)
	assert.Equal(t, "image/png", *ic.MimeType)
	assert.Equal(t, []string{"48x48", "96x96"}, ic.Sizes)
	require.NotNil(t, ic.Theme)
	assert.Equal(t, "dark", *ic.Theme)

	// Packages
	require.Len(t, got.Packages, 1)
	pkg := got.Packages[0]
	assert.Equal(t, "npm", pkg.RegistryType)
	assert.Equal(t, "https://registry.npmjs.org", pkg.RegistryBaseURL)
	assert.Equal(t, "@example/server", pkg.Identifier)
	assert.Equal(t, "1.0.0", pkg.Version)
	assert.Equal(t, "abc123", pkg.FileSHA256)
	assert.Equal(t, "npx", pkg.RunTimeHint)

	// Package transport
	assert.Equal(t, "stdio", pkg.Transport.Type)
	assert.Equal(t, "http://transport-url", pkg.Transport.URL)
	require.Len(t, pkg.Transport.Headers, 1)
	th := pkg.Transport.Headers[0]
	assert.Equal(t, "Authorization", th.Name)
	assert.Equal(t, "auth header", th.Description)
	assert.True(t, th.IsRequired)
	assert.Equal(t, "string", string(th.Format))
	assert.Equal(t, "Bearer {token}", th.Value)
	assert.True(t, th.IsSecret)
	assert.Equal(t, "default-val", th.Default)
	assert.Equal(t, "enter token", th.Placeholder)
	assert.Equal(t, []string{"a", "b"}, th.Choices)
	require.Contains(t, th.Variables, "token")
	tokVar := th.Variables["token"]
	assert.Equal(t, "API token", tokVar.Description)
	assert.True(t, tokVar.IsRequired)
	assert.Equal(t, "string", string(tokVar.Format))
	assert.Equal(t, "tok", tokVar.Value)
	assert.True(t, tokVar.IsSecret)
	assert.Equal(t, "deftoken", tokVar.Default)
	assert.Equal(t, "placeholder", tokVar.Placeholder)
	assert.Equal(t, []string{"x"}, tokVar.Choices)

	// RuntimeArguments
	require.Len(t, pkg.RuntimeArguments, 1)
	ra := pkg.RuntimeArguments[0]
	assert.Equal(t, "positional", string(ra.Type))
	assert.Equal(t, "--port", ra.Name)
	assert.Equal(t, "8080", ra.ValueHint)
	assert.False(t, ra.IsRepeated)
	assert.Equal(t, "port arg", ra.Description)
	assert.True(t, ra.IsRequired)
	assert.Equal(t, "number", string(ra.Format))
	assert.Equal(t, "8080", ra.Value)
	assert.False(t, ra.IsSecret)
	assert.Equal(t, "3000", ra.Default)
	assert.Equal(t, "port number", ra.Placeholder)
	assert.Equal(t, []string{"3000", "8080"}, ra.Choices)
	require.Contains(t, ra.Variables, "portVar")
	pv := ra.Variables["portVar"]
	assert.Equal(t, "port variable", pv.Description)
	assert.False(t, pv.IsRequired)
	assert.Equal(t, "number", string(pv.Format))
	assert.Equal(t, "8080", pv.Value)
	assert.False(t, pv.IsSecret)
	assert.Equal(t, "3000", pv.Default)

	// PackageArguments
	require.Len(t, pkg.PackageArguments, 1)
	pa := pkg.PackageArguments[0]
	assert.Equal(t, "named", string(pa.Type))
	assert.Equal(t, "--config", pa.Name)
	assert.Equal(t, "config.json", pa.ValueHint)
	assert.True(t, pa.IsRepeated)
	assert.Equal(t, "config arg", pa.Description)
	assert.False(t, pa.IsRequired)
	assert.Equal(t, "filepath", string(pa.Format))
	assert.Equal(t, "/etc/config.json", pa.Value)
	assert.False(t, pa.IsSecret)
	assert.Equal(t, "/etc/default.json", pa.Default)
	assert.Equal(t, "path to config", pa.Placeholder)
	assert.Equal(t, []string{"/etc/a.json"}, pa.Choices)
	require.Contains(t, pa.Variables, "configVar")
	cv := pa.Variables["configVar"]
	assert.Equal(t, "config variable", cv.Description)
	assert.True(t, cv.IsRequired)
	assert.Equal(t, "filepath", string(cv.Format))

	// EnvironmentVariables
	require.Len(t, pkg.EnvironmentVariables, 1)
	ev := pkg.EnvironmentVariables[0]
	assert.Equal(t, "API_KEY", ev.Name)
	assert.Equal(t, "API key env var", ev.Description)
	assert.True(t, ev.IsRequired)
	assert.Equal(t, "string", string(ev.Format))
	assert.Equal(t, "key-{secret}", ev.Value)
	assert.True(t, ev.IsSecret)
	assert.Equal(t, "default-key", ev.Default)
	assert.Equal(t, "enter key", ev.Placeholder)
	assert.Equal(t, []string{"k1"}, ev.Choices)
	require.Contains(t, ev.Variables, "secret")
	sv := ev.Variables["secret"]
	assert.Equal(t, "the secret", sv.Description)
	assert.True(t, sv.IsRequired)
	assert.Equal(t, "string", string(sv.Format))
	assert.Equal(t, "s", sv.Value)
	assert.True(t, sv.IsSecret)
	assert.Equal(t, "d", sv.Default)
	assert.Equal(t, "p", sv.Placeholder)
	assert.Equal(t, []string{"c"}, sv.Choices)

	// Remotes
	require.Len(t, got.Remotes, 1)
	rm := got.Remotes[0]
	assert.Equal(t, "sse", rm.Type)
	assert.Equal(t, "http://remote-sse", rm.URL)
	require.Len(t, rm.Headers, 1)
	rh := rm.Headers[0]
	assert.Equal(t, "X-Token", rh.Name)
	assert.Equal(t, "remote header", rh.Description)
	assert.False(t, rh.IsRequired)
	assert.Equal(t, "string", string(rh.Format))
	assert.Equal(t, "tok", rh.Value)
	assert.True(t, rh.IsSecret)
	assert.Equal(t, "def", rh.Default)
	assert.Equal(t, "ph", rh.Placeholder)
	assert.Equal(t, []string{"t1"}, rh.Choices)
	require.Contains(t, rh.Variables, "v1")
	rv := rh.Variables["v1"]
	assert.Equal(t, "var1", rv.Description)
	assert.True(t, rv.IsRequired)
	assert.Equal(t, "string", string(rv.Format))
	assert.Equal(t, "val", rv.Value)
	assert.False(t, rv.IsSecret)
	assert.Equal(t, "d", rv.Default)
	assert.Equal(t, "p", rv.Placeholder)
	assert.Equal(t, []string{"c"}, rv.Choices)
}

func TestToServerJSON_DefaultSchema(t *testing.T) {
	// When spec.Schema is empty, ToServerJSON must fill in CurrentSchemaURL.
	md := kinds.Metadata{Name: "io.example/bare-server", Version: "0.1.0"}
	spec := &kinds.MCPSpec{Description: "bare"}

	got := kinds.ToServerJSON(md, spec)

	require.NotNil(t, got)
	assert.NotEmpty(t, got.Schema, "schema should default to CurrentSchemaURL when spec.Schema is empty")
	assert.Nil(t, got.Repository)
	assert.Empty(t, got.Icons)
	assert.Empty(t, got.Packages)
	assert.Empty(t, got.Remotes)
}
