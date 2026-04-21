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
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
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
// platform-internal *platformtypes.MCPServer by projecting Spec onto the
// upstream apiv0.ServerJSON shape TranslateMCPServer already understands.
// preferRemote=true (or empty Packages) forces remote transport selection;
// otherwise package-first wins when both are defined.
func SpecToPlatformMCPServer(
	ctx context.Context,
	meta v1alpha1.ObjectMeta,
	spec v1alpha1.MCPServerSpec,
	opts MCPServerTranslateOpts,
) (*platformtypes.MCPServer, error) {
	server := mcpServerSpecToServerJSON(meta, spec)
	req := &MCPServerRunRequest{
		RegistryServer: server,
		DeploymentID:   opts.DeploymentID,
		PreferRemote:   opts.PreferRemote || (len(spec.Remotes) > 0 && len(spec.Packages) == 0),
		EnvValues:      nonNilStringMap(opts.EnvValues),
		ArgValues:      nonNilStringMap(opts.ArgValues),
		HeaderValues:   nonNilStringMap(opts.HeaderValues),
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

	agent := &platformtypes.Agent{
		Name:         agentMeta.Name,
		Version:      agentMeta.Version,
		DeploymentID: opts.DeploymentID,
		Deployment: platformtypes.AgentDeployment{
			Image: agentSpec.Image,
			Env:   envValues,
			Port:  DefaultLocalAgentPort,
		},
		ResolvedMCPServers: resolvedConfigs,
	}
	return agent, resolvedServers, nil
}

// SplitDeploymentRuntimeInputs splits a Deployment.Spec.Env map into env /
// arg / header buckets via the legacy ARG_/HEADER_ prefix convention. Keeps
// parity with splitDeploymentRuntimeInputs in deployment_adapter_utils.go so
// legacy and v1alpha1 code paths produce identical runtime wiring.
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

// -----------------------------------------------------------------------------
// Private projection helpers — v1alpha1 → apiv0.ServerJSON shape.
// -----------------------------------------------------------------------------

func mcpServerSpecToServerJSON(meta v1alpha1.ObjectMeta, spec v1alpha1.MCPServerSpec) *apiv0.ServerJSON {
	out := &apiv0.ServerJSON{
		Name:        meta.Name,
		Description: spec.Description,
		Title:       spec.Title,
		Version:     meta.Version,
		WebsiteURL:  spec.WebsiteURL,
	}
	if spec.Repository != nil {
		out.Repository = &model.Repository{
			URL:       spec.Repository.URL,
			Source:    spec.Repository.Source,
			ID:        spec.Repository.ID,
			Subfolder: spec.Repository.Subfolder,
		}
	}
	out.Packages = make([]model.Package, 0, len(spec.Packages))
	for _, p := range spec.Packages {
		out.Packages = append(out.Packages, model.Package{
			RegistryType:         p.RegistryType,
			RegistryBaseURL:      p.RegistryBaseURL,
			Identifier:           p.Identifier,
			Version:              p.Version,
			FileSHA256:           p.FileSHA256,
			RunTimeHint:          p.RuntimeHint,
			Transport:            mcpTransportToModel(p.Transport),
			RuntimeArguments:     mcpArgsToModel(p.RuntimeArguments),
			PackageArguments:     mcpArgsToModel(p.PackageArguments),
			EnvironmentVariables: mcpKVsToModel(p.EnvironmentVariables),
		})
	}
	out.Remotes = make([]model.Transport, 0, len(spec.Remotes))
	for _, r := range spec.Remotes {
		out.Remotes = append(out.Remotes, mcpTransportToModel(r))
	}
	return out
}

func mcpTransportToModel(t v1alpha1.MCPTransport) model.Transport {
	return model.Transport{
		Type:    t.Type,
		URL:     t.URL,
		Headers: mcpKVsToModel(t.Headers),
	}
}

func mcpArgsToModel(args []v1alpha1.MCPArgument) []model.Argument {
	if len(args) == 0 {
		return nil
	}
	out := make([]model.Argument, 0, len(args))
	for _, a := range args {
		out = append(out, model.Argument{
			InputWithVariables: model.InputWithVariables{
				Input: model.Input{
					Description: a.Description,
					IsRequired:  a.IsRequired,
					Format:      model.Format(a.Format),
					Value:       a.Value,
					IsSecret:    a.IsSecret,
					Default:     a.Default,
					Placeholder: a.Placeholder,
					Choices:     a.Choices,
				},
				Variables: mcpVariablesToModel(a.Variables),
			},
			Type:       model.ArgumentType(a.Type),
			Name:       a.Name,
			ValueHint:  a.ValueHint,
			IsRepeated: a.IsRepeated,
		})
	}
	return out
}

func mcpKVsToModel(kvs []v1alpha1.MCPKeyValueInput) []model.KeyValueInput {
	if len(kvs) == 0 {
		return nil
	}
	out := make([]model.KeyValueInput, 0, len(kvs))
	for _, kv := range kvs {
		out = append(out, model.KeyValueInput{
			InputWithVariables: model.InputWithVariables{
				Input: model.Input{
					Description: kv.Description,
					IsRequired:  kv.IsRequired,
					Format:      model.Format(kv.Format),
					Value:       kv.Value,
					IsSecret:    kv.IsSecret,
					Default:     kv.Default,
					Placeholder: kv.Placeholder,
					Choices:     kv.Choices,
				},
				Variables: mcpVariablesToModel(kv.Variables),
			},
			Name: kv.Name,
		})
	}
	return out
}

func mcpVariablesToModel(in map[string]v1alpha1.MCPInputVariable) map[string]model.Input {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]model.Input, len(in))
	for k, v := range in {
		out[k] = model.Input{
			Description: v.Description,
			IsRequired:  v.IsRequired,
			Format:      model.Format(v.Format),
			Value:       v.Value,
			IsSecret:    v.IsSecret,
			Default:     v.Default,
			Placeholder: v.Placeholder,
			Choices:     v.Choices,
		}
	}
	return out
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
