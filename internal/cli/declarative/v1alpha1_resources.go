package declarative

import (
	"context"
	"errors"
	"fmt"
	"strings"

	cliCommon "github.com/agentregistry-dev/agentregistry/internal/cli/common"
	"github.com/agentregistry-dev/agentregistry/internal/cli/scheme"
	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/internal/registry/kinds"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/printer"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

func listLatestAny[T v1alpha1.Object](ctx context.Context, kind string, newObj func() T) ([]any, error) {
	items, err := client.ListAllTyped(
		ctx,
		apiClient,
		kind,
		client.ListOpts{
			Namespace:  v1alpha1.DefaultNamespace,
			LatestOnly: true,
			Limit:      200,
		},
		newObj,
	)
	if err != nil {
		return nil, err
	}

	out := make([]any, 0, len(items))
	for _, item := range items {
		out = append(out, item)
	}
	return out, nil
}

func getAny[T v1alpha1.Object](ctx context.Context, kind, name, version string, newObj func() T) (any, error) {
	return client.GetTyped(ctx, apiClient, kind, v1alpha1.DefaultNamespace, name, version, newObj)
}

func deleteAny[T v1alpha1.Object](ctx context.Context, kind, name, version string, newObj func() T) error {
	targetVersion := version
	if targetVersion == "" {
		obj, err := client.GetTyped(ctx, apiClient, kind, v1alpha1.DefaultNamespace, name, "", newObj)
		if err != nil {
			return err
		}
		targetVersion = obj.GetMetadata().Version
	}
	return apiClient.Delete(ctx, kind, v1alpha1.DefaultNamespace, name, targetVersion)
}

func listDeploymentAny(ctx context.Context) ([]any, error) {
	deployments, err := cliCommon.ListDeployments(ctx, apiClient)
	if err != nil {
		return nil, err
	}
	out := make([]any, 0, len(deployments))
	for _, dep := range deployments {
		out = append(out, dep)
	}
	return out, nil
}

func getDeploymentByTarget(ctx context.Context, name string) (any, error) {
	deployments, err := cliCommon.ListDeployments(ctx, apiClient)
	if err != nil {
		return nil, err
	}
	for _, dep := range deployments {
		if dep != nil && dep.TargetName == name {
			return dep, nil
		}
	}
	return nil, database.ErrNotFound
}

func deleteDeploymentByTarget(ctx context.Context, name, version string) error {
	if version == "" {
		return fmt.Errorf("%w: --version is required when deleting deployments", database.ErrInvalidInput)
	}

	deployments, err := cliCommon.ListDeployments(ctx, apiClient)
	if err != nil {
		return fmt.Errorf("listing deployments: %w", err)
	}

	var matches []*cliCommon.DeploymentRecord
	for _, dep := range deployments {
		if dep == nil {
			continue
		}
		if dep.TargetName == name && dep.TargetVersion == version {
			matches = append(matches, dep)
		}
	}
	if len(matches) == 0 {
		return database.ErrNotFound
	}

	var errs []error
	for _, dep := range matches {
		if err := apiClient.Delete(ctx, v1alpha1.KindDeployment, dep.Namespace, dep.Name, dep.Version); err != nil {
			errs = append(errs, fmt.Errorf("deleting %s (provider %s): %w", dep.ID, dep.ProviderID, err))
		}
	}
	return errorsJoin(errs)
}

func agentToDocument(agent *v1alpha1.Agent) *kinds.Document {
	if agent == nil {
		return nil
	}
	spec := kinds.AgentSpec{
		Image:             agent.Spec.Image,
		Language:          agent.Spec.Language,
		Framework:         agent.Spec.Framework,
		ModelProvider:     agent.Spec.ModelProvider,
		ModelName:         agent.Spec.ModelName,
		Description:       agent.Spec.Description,
		TelemetryEndpoint: agent.Spec.TelemetryEndpoint,
		Title:             agent.Spec.Title,
		WebsiteURL:        agent.Spec.WebsiteURL,
	}
	if agent.Spec.Repository != nil {
		spec.Repository = &kinds.AgentRepository{
			URL:       agent.Spec.Repository.URL,
			Source:    agent.Spec.Repository.Source,
			ID:        agent.Spec.Repository.ID,
			Subfolder: agent.Spec.Repository.Subfolder,
		}
	}
	for _, ref := range agent.Spec.MCPServers {
		spec.McpServers = append(spec.McpServers, kinds.AgentMcpServer{
			Name:    ref.Name,
			Version: ref.Version,
		})
	}
	for _, ref := range agent.Spec.Skills {
		spec.Skills = append(spec.Skills, kinds.AgentSkillRef{
			Name:                 ref.Name,
			RegistrySkillName:    ref.Name,
			RegistrySkillVersion: ref.Version,
		})
	}
	for _, ref := range agent.Spec.Prompts {
		spec.Prompts = append(spec.Prompts, kinds.AgentPromptRef{
			Name:                  ref.Name,
			RegistryPromptName:    ref.Name,
			RegistryPromptVersion: ref.Version,
		})
	}
	for _, pkg := range agent.Spec.Packages {
		spec.Packages = append(spec.Packages, kinds.AgentPackageRef{
			RegistryType: pkg.RegistryType,
			Identifier:   pkg.Identifier,
			Version:      pkg.Version,
			Transport: struct {
				Type string `yaml:"type" json:"type"`
			}{Type: pkg.Transport.Type},
		})
	}
	for _, remote := range agent.Spec.Remotes {
		spec.Remotes = append(spec.Remotes, kinds.AgentRemote{
			Type: remote.Type,
			URL:  remote.URL,
		})
	}

	return &kinds.Document{
		APIVersion: scheme.APIVersion,
		Kind:       v1alpha1.KindAgent,
		Metadata: kinds.Metadata{
			Name:    agent.Metadata.Name,
			Version: agent.Metadata.Version,
		},
		Spec: spec,
	}
}

func mcpToDocument(server *v1alpha1.MCPServer) *kinds.Document {
	if server == nil {
		return nil
	}
	spec := kinds.MCPSpec{
		Description: server.Spec.Description,
		Title:       server.Spec.Title,
		WebsiteURL:  server.Spec.WebsiteURL,
	}
	if server.Spec.Repository != nil {
		spec.Repository = &kinds.MCPRepository{
			URL:       server.Spec.Repository.URL,
			Source:    server.Spec.Repository.Source,
			ID:        server.Spec.Repository.ID,
			Subfolder: server.Spec.Repository.Subfolder,
		}
	}
	for _, icon := range server.Spec.Icons {
		spec.Icons = append(spec.Icons, kinds.MCPIcon{
			Src:      icon.Src,
			MimeType: icon.MimeType,
			Sizes:    icon.Sizes,
			Theme:    icon.Theme,
		})
	}
	for _, pkg := range server.Spec.Packages {
		spec.Packages = append(spec.Packages, kinds.MCPPackage{
			RegistryType:    pkg.RegistryType,
			RegistryBaseURL: pkg.RegistryBaseURL,
			Identifier:      pkg.Identifier,
			Version:         pkg.Version,
			FileSHA256:      pkg.FileSHA256,
			RunTimeHint:     pkg.RuntimeHint,
			Transport: kinds.MCPTransport{
				Type:    pkg.Transport.Type,
				URL:     pkg.Transport.URL,
				Headers: mcpKeyValuesToKinds(pkg.Transport.Headers),
			},
			RuntimeArguments:     mcpArgsToKinds(pkg.RuntimeArguments),
			PackageArguments:     mcpArgsToKinds(pkg.PackageArguments),
			EnvironmentVariables: mcpKeyValuesToKinds(pkg.EnvironmentVariables),
		})
	}
	for _, remote := range server.Spec.Remotes {
		spec.Remotes = append(spec.Remotes, kinds.MCPTransport{
			Type:    remote.Type,
			URL:     remote.URL,
			Headers: mcpKeyValuesToKinds(remote.Headers),
		})
	}
	return &kinds.Document{
		APIVersion: scheme.APIVersion,
		Kind:       v1alpha1.KindMCPServer,
		Metadata: kinds.Metadata{
			Name:    server.Metadata.Name,
			Version: server.Metadata.Version,
		},
		Spec: spec,
	}
}

func skillToDocument(skill *v1alpha1.Skill) *kinds.Document {
	if skill == nil {
		return nil
	}
	spec := kinds.SkillSpec{
		Title:       skill.Spec.Title,
		Category:    skill.Spec.Category,
		Description: skill.Spec.Description,
		WebsiteURL:  skill.Spec.WebsiteURL,
	}
	if skill.Spec.Repository != nil {
		spec.Repository = &kinds.SkillRepository{
			URL:    skill.Spec.Repository.URL,
			Source: skill.Spec.Repository.Source,
		}
	}
	for _, pkg := range skill.Spec.Packages {
		spec.Packages = append(spec.Packages, kinds.SkillPackageRef{
			RegistryType: pkg.RegistryType,
			Identifier:   pkg.Identifier,
			Version:      pkg.Version,
			Transport: struct {
				Type string `yaml:"type" json:"type"`
			}{Type: pkg.Transport.Type},
		})
	}
	for _, remote := range skill.Spec.Remotes {
		spec.Remotes = append(spec.Remotes, kinds.SkillRemoteInfo{URL: remote.URL})
	}
	return &kinds.Document{
		APIVersion: scheme.APIVersion,
		Kind:       v1alpha1.KindSkill,
		Metadata: kinds.Metadata{
			Name:    skill.Metadata.Name,
			Version: skill.Metadata.Version,
		},
		Spec: spec,
	}
}

func promptToDocument(prompt *v1alpha1.Prompt) *kinds.Document {
	if prompt == nil {
		return nil
	}
	return &kinds.Document{
		APIVersion: scheme.APIVersion,
		Kind:       v1alpha1.KindPrompt,
		Metadata: kinds.Metadata{
			Name:    prompt.Metadata.Name,
			Version: prompt.Metadata.Version,
		},
		Spec: kinds.PromptSpec{
			Description: prompt.Spec.Description,
			Content:     prompt.Spec.Content,
		},
	}
}

func providerToDocument(provider *v1alpha1.Provider) *kinds.Document {
	if provider == nil {
		return nil
	}
	return &kinds.Document{
		APIVersion: scheme.APIVersion,
		Kind:       v1alpha1.KindProvider,
		Metadata: kinds.Metadata{
			Name:    provider.Metadata.Name,
			Version: provider.Metadata.Version,
		},
		Spec: kinds.ProviderSpec{
			Platform: provider.Spec.Platform,
			Config:   map[string]any(provider.Spec.Config),
		},
	}
}

func deploymentToDocument(dep *cliCommon.DeploymentRecord) *kinds.Document {
	if dep == nil {
		return nil
	}
	return &kinds.Document{
		APIVersion: scheme.APIVersion,
		Kind:       v1alpha1.KindDeployment,
		Metadata: kinds.Metadata{
			Name:    dep.TargetName,
			Version: dep.TargetVersion,
		},
		Spec: kinds.DeploymentSpec{
			ProviderID:     dep.ProviderID,
			ResourceType:   dep.ResourceType,
			Env:            dep.Env,
			ProviderConfig: dep.ProviderConfig,
			PreferRemote:   dep.PreferRemote,
		},
		Status: deploymentStatus{
			ID:               dep.ID,
			Phase:            dep.Status,
			Origin:           dep.Origin,
			Error:            dep.Error,
			ProviderMetadata: dep.ProviderMetadata,
			DeployedAt:       dep.CreatedAt,
			UpdatedAt:        dep.UpdatedAt,
		},
	}
}

func mcpArgsToKinds(in []v1alpha1.MCPArgument) []kinds.MCPArgument {
	if len(in) == 0 {
		return nil
	}
	out := make([]kinds.MCPArgument, 0, len(in))
	for _, arg := range in {
		out = append(out, kinds.MCPArgument{
			Type:        arg.Type,
			Name:        arg.Name,
			ValueHint:   arg.ValueHint,
			IsRepeated:  arg.IsRepeated,
			Description: arg.Description,
			IsRequired:  arg.IsRequired,
			Format:      arg.Format,
			Value:       arg.Value,
			IsSecret:    arg.IsSecret,
			Default:     arg.Default,
			Placeholder: arg.Placeholder,
			Choices:     arg.Choices,
			Variables:   mcpVariablesToKinds(arg.Variables),
		})
	}
	return out
}

func mcpKeyValuesToKinds(in []v1alpha1.MCPKeyValueInput) []kinds.MCPKeyValueInput {
	if len(in) == 0 {
		return nil
	}
	out := make([]kinds.MCPKeyValueInput, 0, len(in))
	for _, kv := range in {
		out = append(out, kinds.MCPKeyValueInput{
			Name:        kv.Name,
			Description: kv.Description,
			IsRequired:  kv.IsRequired,
			Format:      kv.Format,
			Value:       kv.Value,
			IsSecret:    kv.IsSecret,
			Default:     kv.Default,
			Placeholder: kv.Placeholder,
			Choices:     kv.Choices,
			Variables:   mcpVariablesToKinds(kv.Variables),
		})
	}
	return out
}

func mcpVariablesToKinds(in map[string]v1alpha1.MCPInputVariable) map[string]kinds.MCPInputVariable {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]kinds.MCPInputVariable, len(in))
	for key, value := range in {
		out[key] = kinds.MCPInputVariable{
			Description: value.Description,
			IsRequired:  value.IsRequired,
			Format:      value.Format,
			Value:       value.Value,
			IsSecret:    value.IsSecret,
			Default:     value.Default,
			Placeholder: value.Placeholder,
			Choices:     value.Choices,
		}
	}
	return out
}

func agentRow(agent *v1alpha1.Agent) []string {
	if agent == nil {
		return []string{"<invalid>"}
	}
	return []string{
		printer.TruncateString(agent.Metadata.Name, 40),
		agent.Metadata.Version,
		printer.EmptyValueOrDefault(agent.Spec.Framework, "<none>"),
		printer.EmptyValueOrDefault(agent.Spec.Language, "<none>"),
		printer.EmptyValueOrDefault(agent.Spec.ModelProvider, "<none>"),
		printer.TruncateString(printer.EmptyValueOrDefault(agent.Spec.ModelName, "<none>"), 30),
	}
}

func mcpRow(server *v1alpha1.MCPServer) []string {
	if server == nil {
		return []string{"<invalid>"}
	}
	return []string{
		printer.TruncateString(server.Metadata.Name, 40),
		server.Metadata.Version,
		printer.TruncateString(printer.EmptyValueOrDefault(server.Spec.Description, "<none>"), 60),
	}
}

func skillRow(skill *v1alpha1.Skill) []string {
	if skill == nil {
		return []string{"<invalid>"}
	}
	return []string{
		printer.TruncateString(skill.Metadata.Name, 40),
		skill.Metadata.Version,
		printer.EmptyValueOrDefault(skill.Spec.Category, "<none>"),
		printer.TruncateString(printer.EmptyValueOrDefault(skill.Spec.Description, "<none>"), 60),
	}
}

func promptRow(prompt *v1alpha1.Prompt) []string {
	if prompt == nil {
		return []string{"<invalid>"}
	}
	return []string{
		printer.TruncateString(prompt.Metadata.Name, 40),
		prompt.Metadata.Version,
		printer.TruncateString(printer.EmptyValueOrDefault(prompt.Spec.Description, "<none>"), 60),
	}
}

func providerRow(provider *v1alpha1.Provider) []string {
	if provider == nil {
		return []string{"<invalid>"}
	}
	return []string{provider.Metadata.Name, provider.Spec.Platform}
}

func deploymentRow(dep *cliCommon.DeploymentRecord) []string {
	if dep == nil {
		return []string{"<invalid>"}
	}
	return []string{
		dep.ID,
		dep.TargetName,
		dep.TargetVersion,
		dep.ResourceType,
		dep.ProviderID,
		dep.Status,
	}
}

func deploymentResourceName(targetName, providerID string) string {
	name := strings.ReplaceAll(targetName, "/", "-")
	if providerID == "" {
		return name
	}
	return fmt.Sprintf("%s-%s", name, providerID)
}

func errorsJoin(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}
