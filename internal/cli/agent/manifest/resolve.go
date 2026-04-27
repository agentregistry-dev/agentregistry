package manifest

import (
	"context"
	"fmt"
	"maps"
	"os"

	"github.com/agentregistry-dev/agentregistry/internal/client"
	platformutils "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/utils"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// Resolve projects a v1alpha1.Agent envelope onto the runtime AgentManifest,
// fetching each MCPServer ResourceRef from the registry and translating it
// into a runnable McpServerType (Type="command" or Type="remote") inline.
// The result has no registry-typed entries — every McpServerType is in
// terminal form ready for template render.
//
// Skill and Prompt refs are projected as RegistryRef-bearing entries; the
// runtime resolves those at materialize time (resolveSkillsForRuntime,
// ResolveManifestPrompts) since their resolution involves heavier IO
// (image extraction, content fetch) that the runtime does on demand.
//
// Network calls are performed via apiClient. When agent has no MCPServer
// refs, no network calls are made.
func Resolve(ctx context.Context, apiClient *client.Client, agent *v1alpha1.Agent) (*AgentManifest, error) {
	if agent == nil {
		return nil, fmt.Errorf("agent envelope is required")
	}

	manifest := &AgentManifest{
		Name:              agent.Metadata.Name,
		Image:             agent.Spec.Image,
		Language:          agent.Spec.Language,
		Framework:         agent.Spec.Framework,
		ModelProvider:     agent.Spec.ModelProvider,
		ModelName:         agent.Spec.ModelName,
		Description:       agent.Spec.Description,
		Version:           agent.Metadata.Version,
		TelemetryEndpoint: agent.Spec.TelemetryEndpoint,
	}

	for _, ref := range agent.Spec.MCPServers {
		if apiClient == nil {
			return nil, fmt.Errorf("registry client not initialized; cannot resolve MCP server ref %q", ref.Name)
		}
		serverObj, err := client.GetTyped(
			ctx,
			apiClient,
			v1alpha1.KindMCPServer,
			v1alpha1.DefaultNamespace,
			ref.Name,
			ref.Version,
			func() *v1alpha1.MCPServer { return &v1alpha1.MCPServer{} },
		)
		if err != nil {
			return nil, fmt.Errorf("fetch MCP server %q (version %q): %w", ref.Name, ref.Version, err)
		}
		if serverObj == nil {
			return nil, fmt.Errorf("MCP server %q (version %q) not found in registry", ref.Name, ref.Version)
		}

		entry, err := translateMCPServer(localRefName(ref.Name), serverObj)
		if err != nil {
			return nil, fmt.Errorf("translate MCP server %q: %w", ref.Name, err)
		}
		manifest.McpServers = append(manifest.McpServers, *entry)
	}

	for _, ref := range agent.Spec.Skills {
		manifest.Skills = append(manifest.Skills, SkillRef{
			Name:                 localRefName(ref.Name),
			RegistrySkillName:    ref.Name,
			RegistrySkillVersion: ref.Version,
		})
	}

	for _, ref := range agent.Spec.Prompts {
		manifest.Prompts = append(manifest.Prompts, PromptRef{
			Name:                  localRefName(ref.Name),
			RegistryPromptName:    ref.Name,
			RegistryPromptVersion: ref.Version,
		})
	}

	return manifest, nil
}

// translateMCPServer converts a v1alpha1.MCPServer envelope into a runnable
// McpServerType. Terminal form: Type is always "command" or "remote".
//
// Environment-variable overrides from the local OS env are layered onto
// values declared on the MCPServer's package(s) so the agent runtime can
// supply credentials at run time without modifying the registry resource.
func translateMCPServer(name string, server *v1alpha1.MCPServer) (*McpServerType, error) {
	spec := server.Spec
	if len(spec.Remotes) == 0 && len(spec.Packages) == 0 {
		return nil, fmt.Errorf("server has no remotes or packages")
	}

	envOverrides := collectEnvOverrides(spec.Packages)
	runEnv := make(map[string]string, len(envOverrides))
	maps.Copy(runEnv, envOverrides)

	translated, err := platformutils.TranslateMCPServer(context.Background(), &platformutils.MCPServerRunRequest{
		Name:         server.Metadata.Name,
		Spec:         spec,
		PreferRemote: false,
		EnvValues:    runEnv,
		ArgValues:    map[string]string{},
		HeaderValues: map[string]string{},
	})
	if err != nil {
		return nil, err
	}

	switch translated.MCPServerType {
	case "remote":
		if len(spec.Remotes) == 0 || spec.Remotes[0].URL == "" {
			return nil, fmt.Errorf("remote has no URL")
		}
		headers := make(map[string]string, len(translated.Remote.Headers))
		for _, header := range translated.Remote.Headers {
			headers[header.Name] = header.Value
		}
		return &McpServerType{
			Type:    "remote",
			Name:    name,
			URL:     spec.Remotes[0].URL,
			Headers: headers,
		}, nil
	case "local":
		if translated.Local == nil {
			return nil, fmt.Errorf("local translation missing deployment config")
		}
		buildPath := ""
		if len(spec.Packages) > 0 {
			config, _, err := platformutils.GetRegistryConfig(spec.Packages[0], nil)
			if err != nil {
				return nil, err
			}
			if !config.IsOCI {
				buildPath = "registry/" + name
			}
		}
		return &McpServerType{
			Type:    "command",
			Name:    name,
			Image:   translated.Local.Deployment.Image,
			Build:   buildPath,
			Command: translated.Local.Deployment.Cmd,
			Args:    translated.Local.Deployment.Args,
			Env:     platformutils.EnvMapToStringSlice(translated.Local.Deployment.Env),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported translated server type %q", translated.MCPServerType)
	}
}

// collectEnvOverrides gathers environment variable values from the current
// OS environment for any env vars declared on the package specs. Used so
// the runtime can supply credentials without modifying the registry resource.
func collectEnvOverrides(packages []v1alpha1.MCPPackage) map[string]string {
	overrides := make(map[string]string)
	for _, pkg := range packages {
		for _, envVar := range pkg.EnvironmentVariables {
			if value := os.Getenv(envVar.Name); value != "" {
				overrides[envVar.Name] = value
			}
		}
	}
	return overrides
}
