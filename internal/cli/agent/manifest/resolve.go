package manifest

import (
	"context"
	"fmt"
	"maps"
	"os"

	"github.com/agentregistry-dev/agentregistry/internal/client"
	platformtypes "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/types"
	platformutils "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/utils"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// Resolve pairs a v1alpha1.Agent envelope with the resolved runtime
// form of its MCPServer refs. For each entry in agent.Spec.MCPServers
// it fetches the v1alpha1.MCPServer envelope from the registry,
// translates it via TranslateMCPServer, and produces a
// ResolvedMCPServer (Type="command" or Type="remote") with the bits
// the runtime templates need to render docker-compose + mcp_tools.
//
// Skill and Prompt refs are NOT resolved here — they're materialized
// later (resolveSkillsForRuntime, ResolveManifestPrompts) since their
// resolution involves heavier IO (image extraction, content fetch).
// Callers read agent.Spec.Skills / agent.Spec.Prompts directly when
// they need the refs.
//
// Network calls are performed via apiClient. When agent has no MCPServer
// refs, no network calls are made.
func Resolve(ctx context.Context, apiClient *client.Client, agent *v1alpha1.Agent) (*ResolvedAgent, error) {
	if agent == nil {
		return nil, fmt.Errorf("agent envelope is required")
	}

	resolved := &ResolvedAgent{Agent: agent}

	for _, ref := range agent.Spec.MCPServers {
		if apiClient == nil {
			return nil, fmt.Errorf("registry client not initialized; cannot resolve MCP server ref %q", ref.Name)
		}
		kind := ref.Kind
		if kind == "" {
			kind = v1alpha1.KindMCPServer
		}
		switch kind {
		case v1alpha1.KindMCPServer:
			serverObj, err := client.GetTyped(
				ctx, apiClient,
				v1alpha1.KindMCPServer, v1alpha1.DefaultNamespace, ref.Name, ref.Version,
				func() *v1alpha1.MCPServer { return &v1alpha1.MCPServer{} },
			)
			if err != nil {
				return nil, fmt.Errorf("fetch MCP server %q (version %q): %w", ref.Name, ref.Version, err)
			}
			if serverObj == nil {
				return nil, fmt.Errorf("MCP server %q (version %q) not found in registry", ref.Name, ref.Version)
			}
			entry, err := translateMCPServer(RefBasename(ref.Name), serverObj)
			if err != nil {
				return nil, fmt.Errorf("translate MCP server %q: %w", ref.Name, err)
			}
			resolved.MCPServers = append(resolved.MCPServers, *entry)
		case v1alpha1.KindRemoteMCPServer:
			remoteObj, err := client.GetTyped(
				ctx, apiClient,
				v1alpha1.KindRemoteMCPServer, v1alpha1.DefaultNamespace, ref.Name, ref.Version,
				func() *v1alpha1.RemoteMCPServer { return &v1alpha1.RemoteMCPServer{} },
			)
			if err != nil {
				return nil, fmt.Errorf("fetch RemoteMCPServer %q (version %q): %w", ref.Name, ref.Version, err)
			}
			if remoteObj == nil {
				return nil, fmt.Errorf("RemoteMCPServer %q (version %q) not found in registry", ref.Name, ref.Version)
			}
			entry, err := translateRemoteMCPServerRef(RefBasename(ref.Name), remoteObj)
			if err != nil {
				return nil, fmt.Errorf("translate RemoteMCPServer %q: %w", ref.Name, err)
			}
			resolved.MCPServers = append(resolved.MCPServers, *entry)
		default:
			return nil, fmt.Errorf("unsupported MCP server ref kind %q for ref %q", ref.Kind, ref.Name)
		}
	}

	return resolved, nil
}

// translateRemoteMCPServerRef projects a v1alpha1.RemoteMCPServer onto a
// ResolvedMCPServer with Type="remote".
func translateRemoteMCPServerRef(name string, server *v1alpha1.RemoteMCPServer) (*ResolvedMCPServer, error) {
	translated, err := platformutils.TranslateRemoteMCPServer(context.Background(), &platformutils.RemoteMCPServerRunRequest{
		Name: server.Metadata.Name,
		Spec: server.Spec,
	})
	if err != nil {
		return nil, err
	}
	if translated.Remote == nil {
		return nil, fmt.Errorf("remote has no URL")
	}
	headers := make(map[string]string, len(translated.Remote.Headers))
	for _, h := range translated.Remote.Headers {
		headers[h.Name] = h.Value
	}
	return &ResolvedMCPServer{
		Type:    "remote",
		Name:    name,
		URL:     server.Spec.Remote.URL,
		Headers: headers,
	}, nil
}

// translateMCPServer converts a bundled v1alpha1.MCPServer envelope into a
// ResolvedMCPServer with Type="command". Remote endpoints are resolved
// separately via translateRemoteMCPServerRef when AgentSpec.MCPServers
// references a RemoteMCPServer kind.
//
// Environment-variable overrides from the local OS env are layered onto
// values declared on the MCPServer's package(s) so the agent runtime can
// supply credentials at run time without modifying the registry resource.
func translateMCPServer(name string, server *v1alpha1.MCPServer) (*ResolvedMCPServer, error) {
	spec := server.Spec
	if len(spec.Packages) == 0 {
		return nil, fmt.Errorf("server has no packages")
	}

	envOverrides := collectEnvOverrides(spec.Packages)
	runEnv := make(map[string]string, len(envOverrides))
	maps.Copy(runEnv, envOverrides)

	translated, err := platformutils.TranslateMCPServer(context.Background(), &platformutils.MCPServerRunRequest{
		Name:         server.Metadata.Name,
		Spec:         spec,
		EnvValues:    runEnv,
		ArgValues:    map[string]string{},
		HeaderValues: map[string]string{},
	})
	if err != nil {
		return nil, err
	}

	if translated.MCPServerType != platformtypes.MCPServerTypeLocal || translated.Local == nil {
		return nil, fmt.Errorf("expected local translation for bundled MCPServer, got %q", translated.MCPServerType)
	}
	buildPath := ""
	config, _, err := platformutils.GetRegistryConfig(spec.Packages[0], nil)
	if err != nil {
		return nil, err
	}
	if !config.IsOCI {
		buildPath = "registry/" + name
	}
	return &ResolvedMCPServer{
		Type:    "command",
		Name:    name,
		Image:   translated.Local.Deployment.Image,
		Build:   buildPath,
		Command: translated.Local.Deployment.Cmd,
		Args:    translated.Local.Deployment.Args,
		Env:     platformutils.EnvMapToStringSlice(translated.Local.Deployment.Env),
	}, nil
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
