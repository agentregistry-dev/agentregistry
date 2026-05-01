package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/constants"
	platformtypes "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/types"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// MCPServerTranslateOpts bundles knobs for SpecToPlatformMCPServer that vary
// per-adapter. Zero values mean "use the natural fallback" (preferRemote
// defaults to spec-driven; Namespace falls back to meta.Namespace).
type MCPServerTranslateOpts struct {
	DeploymentID string
	PreferRemote bool
	// Namespace, when non-empty, overrides meta.Namespace on the emitted
	// platform MCPServer. k8s callers set it to the provider's runtime
	// namespace so label selectors line up; local callers usually leave it
	// blank.
	Namespace    string
	EnvValues    map[string]string
	ArgValues    map[string]string
	HeaderValues map[string]string
}

// SpecToPlatformMCPServer translates a v1alpha1 MCPServer envelope into the
// platform-internal *platformtypes.MCPServer by calling TranslateMCPServer
// directly on the v1alpha1 types. preferRemote=true (or empty Packages)
// forces remote transport selection; otherwise package-first wins when both
// are defined.
func SpecToPlatformMCPServer(
	ctx context.Context,
	meta v1alpha1.ObjectMeta,
	spec v1alpha1.MCPServerSpec,
	opts MCPServerTranslateOpts,
) (*platformtypes.MCPServer, error) {
	req := &MCPServerRunRequest{
		Name:         meta.Name,
		Spec:         spec,
		DeploymentID: opts.DeploymentID,
		PreferRemote: opts.PreferRemote || (len(spec.Remotes) > 0 && len(spec.Packages) == 0),
		EnvValues:    nonNilStringMap(opts.EnvValues),
		ArgValues:    nonNilStringMap(opts.ArgValues),
		HeaderValues: nonNilStringMap(opts.HeaderValues),
	}
	platformServer, err := TranslateMCPServer(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("translate mcp server %s@%s: %w", meta.Name, meta.Version, err)
	}
	if opts.Namespace != "" {
		platformServer.Namespace = opts.Namespace
	} else if meta.Namespace != "" && platformServer.Namespace == "" {
		platformServer.Namespace = meta.Namespace
	}
	return platformServer, nil
}

// AgentTranslateOpts bundles knobs for SpecToPlatformAgent.
type AgentTranslateOpts struct {
	DeploymentID string
	// Namespace is the target runtime namespace — populates KAGENT_NAMESPACE
	// and propagates to every nested MCPServer the agent references. Empty ⇒
	// meta.Namespace ⇒ v1alpha1.DefaultNamespace.
	Namespace string
	// KagentURL is the KAGENT_URL env value the agent process gets.
	// "http://localhost" for local, "http://kagent-controller.kagent.svc
	// .cluster.local" for in-cluster, etc.
	KagentURL string
	// DeploymentEnv is the raw Deployment.Spec.Env map pre-split — callers
	// pass it as-is; SpecToPlatformAgent treats it as plain env overrides.
	// Use SplitDeploymentRuntimeInputs upstream if the deployment encodes
	// ARG_/HEADER_ prefixes.
	DeploymentEnv map[string]string
	// Getter resolves AgentSpec.MCPServers refs to v1alpha1.MCPServer objects.
	Getter v1alpha1.GetterFunc
}

// SpecToPlatformAgent translates a v1alpha1 Agent envelope + Deployment
// overrides into the platform-internal *platformtypes.Agent plus the set of
// resolved MCPServers that should be deployed alongside it. Nested
// AgentSpec.MCPServers refs are fetched via opts.Getter; dangling refs
// surface as v1alpha1.ErrDanglingRef.
func SpecToPlatformAgent(
	ctx context.Context,
	agentMeta v1alpha1.ObjectMeta,
	agentSpec v1alpha1.AgentSpec,
	opts AgentTranslateOpts,
) (*platformtypes.Agent, []*platformtypes.MCPServer, error) {
	envValues := nonNilStringMap(opts.DeploymentEnv)
	if envValues[constants.EnvKagentNamespace] == "" {
		switch {
		case opts.Namespace != "":
			envValues[constants.EnvKagentNamespace] = opts.Namespace
		case agentMeta.Namespace != "":
			envValues[constants.EnvKagentNamespace] = agentMeta.Namespace
		default:
			envValues[constants.EnvKagentNamespace] = v1alpha1.DefaultNamespace
		}
	}
	if opts.KagentURL != "" {
		envValues[constants.EnvKagentURL] = opts.KagentURL
	}
	envValues[constants.EnvKagentName] = agentMeta.Name
	envValues[constants.EnvAgentName] = agentMeta.Name
	envValues[constants.EnvModelProvider] = agentSpec.ModelProvider
	envValues[constants.EnvModelName] = agentSpec.ModelName

	var (
		resolvedServers []*platformtypes.MCPServer
		resolvedConfigs []platformtypes.ResolvedMCPServerConfig
	)
	for i, ref := range agentSpec.MCPServers {
		normalized := ref
		normalized.Kind = v1alpha1.KindMCPServer
		if normalized.Namespace == "" {
			normalized.Namespace = agentMeta.Namespace
		}
		if opts.Getter == nil {
			return nil, nil, fmt.Errorf("spec.mcpServers[%d]: getter required to resolve ref", i)
		}
		obj, err := opts.Getter(ctx, normalized)
		if err != nil {
			return nil, nil, fmt.Errorf("spec.mcpServers[%d] resolve %s/%s: %w", i, normalized.Namespace, normalized.Name, err)
		}
		mcp, ok := obj.(*v1alpha1.MCPServer)
		if !ok || mcp == nil {
			return nil, nil, fmt.Errorf("spec.mcpServers[%d]: getter returned unexpected type for %s/%s", i, normalized.Namespace, normalized.Name)
		}
		platformServer, err := SpecToPlatformMCPServer(ctx, mcp.Metadata, mcp.Spec, MCPServerTranslateOpts{
			DeploymentID: opts.DeploymentID,
			Namespace:    opts.Namespace,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("spec.mcpServers[%d]: %w", i, err)
		}
		resolvedServers = append(resolvedServers, platformServer)
		resolvedConfigs = append(resolvedConfigs, mcpServerConfigFromSpec(mcp.Metadata.Name, mcp.Spec, opts.DeploymentID))
	}

	if len(resolvedConfigs) > 0 {
		encoded, err := json.Marshal(resolvedConfigs)
		if err != nil {
			return nil, nil, fmt.Errorf("marshal MCP servers config: %w", err)
		}
		envValues[constants.EnvMCPServersConfig] = string(encoded)
	}

	var image string
	if agentSpec.Source != nil {
		image = agentSpec.Source.Image
	}
	agent := &platformtypes.Agent{
		Name:         agentMeta.Name,
		Version:      agentMeta.Version,
		DeploymentID: opts.DeploymentID,
		Deployment: platformtypes.AgentDeployment{
			Image: image,
			Env:   envValues,
			Port:  DefaultLocalAgentPort,
		},
		ResolvedMCPServers: resolvedConfigs,
	}
	return agent, resolvedServers, nil
}

// SplitDeploymentRuntimeInputs splits a Deployment.Spec.Env map into env /
// arg / header buckets via the ARG_/HEADER_ prefix convention. Prefix-free
// keys are plain env; ARG_<name> and HEADER_<name> route to arg and header
// overrides respectively.
func SplitDeploymentRuntimeInputs(input map[string]string) (env, args, headers map[string]string) {
	env = map[string]string{}
	args = map[string]string{}
	headers = map[string]string{}
	for key, value := range input {
		switch {
		case strings.HasPrefix(key, "ARG_"):
			if name := strings.TrimPrefix(key, "ARG_"); name != "" {
				args[name] = value
			}
		case strings.HasPrefix(key, "HEADER_"):
			if name := strings.TrimPrefix(key, "HEADER_"); name != "" {
				headers[name] = value
			}
		default:
			env[key] = value
		}
	}
	return env, args, headers
}

// mcpServerConfigFromSpec builds the per-server entry injected into the
// MCP_SERVERS_CONFIG env var on the agent. Remote transport wins when the
// spec offers one; otherwise we tag the entry as "command" for the agent
// process to dial via the gateway.
func mcpServerConfigFromSpec(name string, spec v1alpha1.MCPServerSpec, deploymentID string) platformtypes.ResolvedMCPServerConfig {
	cfg := platformtypes.ResolvedMCPServerConfig{
		Name: GenerateInternalNameForDeployment(name, deploymentID),
		Type: "command",
	}
	if len(spec.Remotes) > 0 {
		cfg.Type = "remote"
		cfg.URL = spec.Remotes[0].URL
		if len(spec.Remotes[0].Headers) > 0 {
			headers := make(map[string]string, len(spec.Remotes[0].Headers))
			for _, h := range spec.Remotes[0].Headers {
				headers[h.Name] = h.Value
			}
			cfg.Headers = headers
		}
	}
	return cfg
}

func nonNilStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	if len(in) == 0 {
		return out
	}
	maps.Copy(out, in)
	return out
}
