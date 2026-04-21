package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/constants"
	platformtypes "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/types"
	"github.com/agentregistry-dev/agentregistry/internal/registry/platforms/utils"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

// specToPlatformMCPServer translates a v1alpha1.MCPServer envelope into the
// platform-internal *platformtypes.MCPServer by projecting Spec onto the
// upstream apiv0.ServerJSON shape that utils.TranslateMCPServer already
// understands. namespace is honored when non-empty — kagent CRDs need it
// for label selectors + cross-namespace routing.
func specToPlatformMCPServer(
	ctx context.Context,
	meta v1alpha1.ObjectMeta,
	spec v1alpha1.MCPServerSpec,
	deploymentID string,
	preferRemote bool,
	envValues, argValues, headerValues map[string]string,
	namespace string,
) (*platformtypes.MCPServer, error) {
	server := mcpServerSpecToServerJSON(meta, spec)
	req := &utils.MCPServerRunRequest{
		RegistryServer: server,
		DeploymentID:   deploymentID,
		PreferRemote:   preferRemote || (len(spec.Remotes) > 0 && len(spec.Packages) == 0),
		EnvValues:      nonNilStringMap(envValues),
		ArgValues:      nonNilStringMap(argValues),
		HeaderValues:   nonNilStringMap(headerValues),
	}
	platformServer, err := utils.TranslateMCPServer(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("translate mcp server %s@%s: %w", meta.Name, meta.Version, err)
	}
	if namespace != "" {
		platformServer.Namespace = namespace
	} else if meta.Namespace != "" && platformServer.Namespace == "" {
		platformServer.Namespace = meta.Namespace
	}
	return platformServer, nil
}

// specToPlatformAgent translates a v1alpha1.Agent envelope + Deployment
// overrides into the platform-internal *platformtypes.Agent plus the set of
// resolved MCPServers that should be deployed alongside it. Nested
// AgentSpec.MCPServers refs are fetched via the supplied GetterFunc;
// dangling refs surface as ErrDanglingRef.
//
// namespace is the kubernetes namespace to pin on every produced resource —
// normally derived from Provider.Spec.Config.namespace or EnvKagentNamespace.
func specToPlatformAgent(
	ctx context.Context,
	agentMeta v1alpha1.ObjectMeta,
	agentSpec v1alpha1.AgentSpec,
	deploymentID string,
	deploymentEnv map[string]string,
	getter v1alpha1.GetterFunc,
	namespace string,
) (*platformtypes.Agent, []*platformtypes.MCPServer, error) {
	envValues := nonNilStringMap(deploymentEnv)
	if envValues[constants.EnvKagentNamespace] == "" {
		switch {
		case namespace != "":
			envValues[constants.EnvKagentNamespace] = namespace
		case agentMeta.Namespace != "":
			envValues[constants.EnvKagentNamespace] = agentMeta.Namespace
		default:
			envValues[constants.EnvKagentNamespace] = v1alpha1.DefaultNamespace
		}
	}
	envValues[constants.EnvKagentURL] = "http://kagent-controller.kagent.svc.cluster.local"
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
		if getter == nil {
			return nil, nil, fmt.Errorf("spec.mcpServers[%d]: getter required to resolve ref", i)
		}
		obj, err := getter(ctx, normalized)
		if err != nil {
			return nil, nil, fmt.Errorf("spec.mcpServers[%d] resolve %s/%s: %w", i, normalized.Namespace, normalized.Name, err)
		}
		mcp, ok := obj.(*v1alpha1.MCPServer)
		if !ok || mcp == nil {
			return nil, nil, fmt.Errorf("spec.mcpServers[%d]: getter returned unexpected type for %s/%s", i, normalized.Namespace, normalized.Name)
		}
		platformServer, err := specToPlatformMCPServer(ctx, mcp.Metadata, mcp.Spec, deploymentID, false, nil, nil, nil, namespace)
		if err != nil {
			return nil, nil, fmt.Errorf("spec.mcpServers[%d]: %w", i, err)
		}
		resolvedServers = append(resolvedServers, platformServer)
		resolvedConfigs = append(resolvedConfigs, mcpServerConfigFromSpec(mcp.Metadata.Name, mcp.Spec, deploymentID))
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
		DeploymentID: deploymentID,
		Deployment: platformtypes.AgentDeployment{
			Image: agentSpec.Image,
			Env:   envValues,
			Port:  utils.DefaultLocalAgentPort,
		},
		ResolvedMCPServers: resolvedConfigs,
	}
	return agent, resolvedServers, nil
}

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

func mcpServerConfigFromSpec(name string, spec v1alpha1.MCPServerSpec, deploymentID string) platformtypes.ResolvedMCPServerConfig {
	cfg := platformtypes.ResolvedMCPServerConfig{
		Name: utils.GenerateInternalNameForDeployment(name, deploymentID),
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

func splitDeploymentRuntimeInputs(input map[string]string) (env, args, headers map[string]string) {
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
